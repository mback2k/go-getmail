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
	err   error
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

func (c *fetchConfig) watch(ctx context.Context) error {
	defer func(c *fetchConfig, s fetchState) {
		c.state = s
	}(c, c.state)
	c.state = watchingState

	c.log().Info("Begin idling")

	ctx, cancel := context.WithCancel(ctx)
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
				c.handle(cancel)
			}
		case err := <-errors:
			c.log().Warnf("Not idling anymore: %w", err)
			return err
		}
	}
}

func (c *fetchConfig) handle(cancel context.CancelFunc) {
	defer func(c *fetchConfig, s fetchState) {
		c.state = s
	}(c, c.state)
	c.state = handlingState

	c.log().Info("Begin handling")

	err := c.Source.openIMAP()
	if err != nil {
		c.log().Warnf("Source connection failed: %w", err)
		cancel()
		return
	}
	defer c.Source.closeIMAP()

	err = c.Target.openIMAP()
	if err != nil {
		c.log().Warnf("Target connection failed: %w", err)
		cancel()
		return
	}
	defer c.Target.closeIMAP()

	errors := make(chan error, 1)
	messages := make(chan *imap.Message, 100)
	deletes := make(chan uint32, 100)

	go c.Source.fetchMessages(messages, errors)
	go c.Target.storeMessages(messages, deletes, errors)
	go c.Source.cleanMessages(deletes, errors)

	for {
		err, more := <-errors
		if err != nil {
			c.log().Warnf("Message handling failed: %w", err)
			cancel()
		}
		if !more {
			c.log().Info("Message handling finished")
			break
		}
	}
}

func (s *fetchSource) fetchMessages(messages chan *imap.Message, errors chan<- error) {
	update, err := s.selectIMAP()
	if err != nil {
		errors <- err
		close(messages)
		return
	}

	if update.Mailbox.Messages < 1 {
		close(messages)
		return
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(1, update.Mailbox.Messages)

	errors <- s.imapconn.Fetch(seqset, []imap.FetchItem{
		"UID", "FLAGS", "INTERNALDATE", "BODY[]"}, messages)
}

func (t *fetchTarget) storeMessages(messages <-chan *imap.Message, deletes chan<- uint32, errors chan<- error) {
	defer close(deletes)

	section, err := imap.ParseBodySectionName("BODY[]")
	if err != nil {
		errors <- err
		return
	}

	update, err := t.selectIMAP()
	if err != nil {
		errors <- err
		return
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
			errors <- err
			return
		}

		t.config.total++
		deletes <- msg.Uid
	}
}

func (s *fetchSource) cleanMessages(deletes <-chan uint32, errors chan<- error) {
	defer close(errors)

	seqset := new(imap.SeqSet)
	for uid := range deletes {
		s.config.log().Infof("Deleting message: %d", uid)

		seqset.AddNum(uid)
	}

	if seqset.Empty() {
		return
	}

	err := s.imapconn.UidStore(seqset, imap.AddFlags, []interface{}{imap.DeletedFlag}, nil)
	if err != nil {
		errors <- err
	}
}

func (c *fetchConfig) run(ctx context.Context, done chan<- *fetchConfig) {
	defer c.done(done)
	c.err = c.init()
	if c.err == nil {
		c.err = c.watch(ctx)
	}
}

func (c *fetchConfig) done(done chan<- *fetchConfig) {
	err := c.close()
	if c.err == nil {
		c.err = err
	}
	done <- c
}

func (c *fetchConfig) log() *log.Entry {
	return log.WithFields(log.Fields{
		"name":  c.Name,
		"state": c.state,
	})
}
