/*
	go-getmail - Retrieve and forward e-mails between IMAP servers.
	Copyright (C) 2019  Marc Hoersken <info@marc-hoersken.de>

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package main

import (
	"context"

	"golang.org/x/sync/errgroup"

	imap "github.com/emersion/go-imap"
	idle "github.com/emersion/go-imap-idle"
	client "github.com/emersion/go-imap/client"

	log "github.com/sirupsen/logrus"
)

// FetchServer contains the IMAP credentials.
type FetchServer struct {
	Server   string
	Username string
	Password string
	Mailbox  string

	config   *fetchConfig
	imapconn *client.Client
}

type fetchSource struct {
	FetchServer `mapstructure:"IMAP"`

	idleconn *client.Client
	idle     *idle.Client
	updates  chan client.Update
}

type fetchTarget struct {
	FetchServer `mapstructure:"IMAP"`
}

type fetchState int

const (
	initialState    = (fetchState)(0 << 0)
	connectingState = (fetchState)(1 << 0)
	connectedState  = (fetchState)(1 << 1)
	watchingState   = (fetchState)(1 << 2)
	handlingState   = (fetchState)(1 << 3)
	shutdownState   = (fetchState)(1 << 4)
)

type fetchConfig struct {
	Name   string
	Source fetchSource
	Target fetchTarget

	state fetchState
	total uint64
	ctx   context.Context
}

func (s *FetchServer) open() (*client.Client, error) {
	con, err := client.DialTLS(s.Server, nil)
	if err != nil {
		return nil, err
	}
	err = con.Login(s.Username, s.Password)
	if err != nil {
		return nil, err
	}
	return con, nil
}

func (s *FetchServer) openIMAP() error {
	con, err := s.open()
	if err != nil {
		return err
	}
	s.imapconn = con
	return nil
}

func (s *fetchSource) openIDLE() error {
	con, err := s.open()
	if err != nil {
		return err
	}
	s.idleconn = con
	return nil
}

func (s *FetchServer) selectIMAP() (*client.MailboxUpdate, error) {
	status, err := s.imapconn.Select(s.Mailbox, false)
	update := &client.MailboxUpdate{Mailbox: status}
	return update, err
}

func (s *fetchSource) selectIDLE() (*client.MailboxUpdate, error) {
	status, err := s.idleconn.Select(s.Mailbox, true)
	update := &client.MailboxUpdate{Mailbox: status}
	return update, err
}

func (s *fetchSource) initIDLE() error {
	update, err := s.selectIDLE()
	if err != nil {
		return err
	}
	updates := make(chan client.Update, 1)
	updates <- update

	s.idle = idle.NewClient(s.idleconn)
	s.idleconn.Updates = updates
	s.updates = updates
	return nil
}

func (c *fetchConfig) init() error {
	c.Source.config = c
	c.Target.config = c
	c.state = connectingState
	err := c.Source.openIMAP()
	if err != nil {
		return err
	}
	err = c.Source.closeIMAP()
	if err != nil {
		return err
	}
	err = c.Target.openIMAP()
	if err != nil {
		return err
	}
	err = c.Target.closeIMAP()
	if err != nil {
		return err
	}
	err = c.Source.openIDLE()
	if err != nil {
		return err
	}
	err = c.Source.initIDLE()
	if err != nil {
		return err
	}
	c.state = connectedState
	return err
}

func (s *FetchServer) closeIMAP() error {
	if s.imapconn == nil {
		return nil
	}
	err := s.imapconn.Logout()
	if err != nil {
		return err
	}
	s.imapconn = nil
	return nil
}

func (s *fetchSource) closeIDLE() error {
	if s.idleconn == nil {
		return nil
	}
	err := s.idleconn.Logout()
	if err != nil {
		return err
	}
	s.idleconn = nil
	return nil
}

func (c *fetchConfig) close() error {
	c.state = shutdownState
	err := c.Source.closeIDLE()
	if err != nil {
		return err
	}
	err = c.Source.closeIMAP()
	if err != nil {
		return err
	}
	err = c.Target.closeIMAP()
	if err != nil {
		return err
	}
	c.state = initialState
	return nil
}

func (c *fetchConfig) watch() error {
	defer func(c *fetchConfig, s fetchState) {
		c.state = s
	}(c, c.state)
	c.state = watchingState

	c.log().Info("Begin idling")

	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()

	errors := make(chan error, 1)
	go func() {
		errors <- c.Source.idle.IdleWithFallback(ctx.Done(), 0)
	}()
	for {
		select {
		case update := <-c.Source.updates:
			c.log().Infof("New update: %#v", update)
			_, ok := update.(*client.MailboxUpdate)
			if ok {
				err := c.handle()
				if err != nil {
					return err
				}
			}
		case err := <-errors:
			c.log().Warnf("Not idling anymore: %v", err)
			return err
		}
	}
}

func (c *fetchConfig) handle() error {
	defer func(c *fetchConfig, s fetchState) {
		c.state = s
	}(c, c.state)
	c.state = handlingState

	c.log().Info("Begin handling")

	err := c.Source.openIMAP()
	if err != nil {
		c.log().Warnf("Source connection failed: %v", err)
		return err
	}
	defer c.Source.closeIMAP()

	err = c.Target.openIMAP()
	if err != nil {
		c.log().Warnf("Target connection failed: %v", err)
		return err
	}
	defer c.Target.closeIMAP()

	messages := make(chan *imap.Message, 100)
	deletes := make(chan uint32, 100)

	var g errgroup.Group
	g.Go(func() error {
		return c.Source.fetchMessages(messages)
	})
	g.Go(func() error {
		return c.Target.storeMessages(messages, deletes)
	})
	g.Go(func() error {
		return c.Source.cleanMessages(deletes)
	})

	err = g.Wait()
	if err != nil {
		c.log().Warnf("Message handling failed: %v", err)
	} else {
		c.log().Info("Message handling finished")
	}
	return err
}

func (s *fetchSource) fetchMessages(messages chan *imap.Message) error {
	update, err := s.selectIMAP()
	if err != nil {
		close(messages)
		return err
	}

	if update.Mailbox.Messages < 1 {
		close(messages)
		return nil
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(1, update.Mailbox.Messages)

	return s.imapconn.Fetch(seqset, []imap.FetchItem{
		"UID", "FLAGS", "INTERNALDATE", "BODY[]"}, messages)
}

func (t *fetchTarget) storeMessages(messages <-chan *imap.Message, deletes chan<- uint32) error {
	defer close(deletes)

	section, err := imap.ParseBodySectionName("BODY[]")
	if err != nil {
		return err
	}

	update, err := t.selectIMAP()
	if err != nil {
		return err
	}

	for msg := range messages {
		t.config.log().Infof("Handling message: %d", msg.Uid)

		deleted := false
		flags := []string{}
		for _, flag := range msg.Flags {
			switch flag {
			case imap.DeletedFlag:
				deleted = true
				break
			case imap.RecentFlag:
				continue
			case imap.SeenFlag:
				continue
			default:
				flags = append(flags, flag)
			}
		}
		if deleted {
			t.config.log().Infof("Ignoring message: %d", msg.Uid)
			continue
		}

		t.config.log().Infof("Storing message: %d", msg.Uid)

		body := msg.GetBody(section)
		err := t.imapconn.Append(update.Mailbox.Name, flags, msg.InternalDate, body)
		if err != nil {
			return err
		}

		t.config.total++
		deletes <- msg.Uid
	}

	return nil
}

func (s *fetchSource) cleanMessages(deletes <-chan uint32) error {
	seqset := new(imap.SeqSet)
	for uid := range deletes {
		s.config.log().Infof("Deleting message: %d", uid)

		seqset.AddNum(uid)
	}

	if seqset.Empty() {
		return nil
	}

	return s.imapconn.UidStore(seqset, imap.AddFlags,
		[]interface{}{imap.DeletedFlag}, nil)
}

func (c *fetchConfig) run() error {
	err := c.init()
	if err != nil {
		return err
	}
	defer c.close()
	return c.watch()
}

func (c *fetchConfig) log() *log.Entry {
	return log.WithFields(log.Fields{
		"name":  c.Name,
		"state": c.state,
	})
}
