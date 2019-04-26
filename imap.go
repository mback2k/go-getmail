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
	"log"
	"reflect"

	imap "github.com/emersion/go-imap"
	idle "github.com/emersion/go-imap-idle"
	client "github.com/emersion/go-imap/client"
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
	fetchingState   = (fetchState)(1 << 3)
	shutdownState   = (fetchState)(1 << 4)
)

type fetchConfig struct {
	Name   string
	Source fetchSource
	Target fetchTarget

	state fetchState
	err   error
}

func (c *FetchServer) open() (*client.Client, error) {
	con, err := client.DialTLS(c.Server, nil)
	if err != nil {
		return nil, err
	}
	err = con.Login(c.Username, c.Password)
	if err != nil {
		return nil, err
	}
	return con, nil
}

func (c *FetchServer) openIMAP() error {
	con, err := c.open()
	if err != nil {
		return err
	}
	c.imapconn = con
	return nil
}

func (c *fetchSource) openIDLE() error {
	con, err := c.open()
	if err != nil {
		return err
	}
	c.idleconn = con
	return nil
}

func (c *FetchServer) selectIMAP() (*client.MailboxUpdate, error) {
	status, err := c.imapconn.Select(c.Mailbox, false)
	update := &client.MailboxUpdate{Mailbox: status}
	return update, err
}

func (c *fetchSource) selectIDLE() (*client.MailboxUpdate, error) {
	status, err := c.idleconn.Select(c.Mailbox, false)
	update := &client.MailboxUpdate{Mailbox: status}
	return update, err
}

func (c *fetchSource) startIDLE() error {
	update, err := c.selectIDLE()
	if err != nil {
		return err
	}
	updates := make(chan client.Update, 1)
	updates <- update

	c.idle = idle.NewClient(c.idleconn)
	c.idleconn.Updates = updates
	c.updates = updates
	return nil
}

func (c *fetchConfig) start() error {
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
	err = c.Source.startIDLE()
	if err != nil {
		return err
	}
	c.state = connectedState
	return err
}

func (c *FetchServer) closeIMAP() error {
	if c.imapconn == nil {
		return nil
	}
	err := c.imapconn.Logout()
	if err != nil {
		return err
	}
	c.imapconn = nil
	return nil
}

func (c *fetchSource) closeIDLE() error {
	if c.idleconn == nil {
		return nil
	}
	err := c.idleconn.Logout()
	if err != nil {
		return err
	}
	c.idleconn = nil
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
	c.state = watchingState
	log.Println(c.Name, "[", c.state, "]:", "Begin idling")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errors := make(chan error, 1)
	go func() {
		errors <- c.Source.idle.IdleWithFallback(ctx.Done(), 0)
	}()
	for {
		select {
		case update := <-c.Source.updates:
			log.Println(c.Name, "[", c.state, "]:", "New update:", reflect.TypeOf(update))
			_, ok := update.(*client.MailboxUpdate)
			if ok {
				c.state = fetchingState
				err := c.handle()
				if err != nil {
					return err
				}
				c.state = watchingState
			}
		case err := <-errors:
			log.Println(c.Name, "[", c.state, "]:", "Not idling anymore", err)
			return err
		}
	}
}

func (c *fetchConfig) handle() error {
	log.Println(c.Name, "[", c.state, "]:", "Begin handling")

	err := c.Source.openIMAP()
	if err != nil {
		return err
	}
	defer c.Source.closeIMAP()

	err = c.Target.openIMAP()
	if err != nil {
		return err
	}
	defer c.Target.closeIMAP()

	errors := make(chan error, 1)
	messages := make(chan *imap.Message, 1)
	deletes := make(chan uint32, 1)

	go c.Source.fetchMessages(messages, errors)
	go c.Target.storeMessages(messages, deletes, errors)
	go c.Source.cleanMessages(deletes, errors)

	for {
		err, more := <-errors
		if err != nil {
			log.Println(c.Name, "[", c.state, "]:", "Message handling failed", err)
			return err
		}
		if !more {
			log.Println(c.Name, "[", c.state, "]:", "Message handling successful")
			return nil
		}
	}
}

func (c *fetchSource) fetchMessages(messages chan *imap.Message, errors chan<- error) {
	update, err := c.selectIMAP()
	if err != nil {
		errors <- err
		return
	}

	if update.Mailbox.Messages < 1 {
		close(errors)
		return
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(1, update.Mailbox.Messages)

	errors <- c.imapconn.Fetch(seqset, []imap.FetchItem{
		"UID", "FLAGS", "INTERNALDATE", "BODY[]"}, messages)
}

func (c *fetchTarget) storeMessages(messages <-chan *imap.Message, deletes chan<- uint32, errors chan<- error) {
	section, err := imap.ParseBodySectionName("BODY[]")
	if err != nil {
		errors <- err
		return
	}

	update, err := c.selectIMAP()
	if err != nil {
		errors <- err
		return
	}

	for msg := range messages {
		log.Println(c.config.Name, "[", c.config.state, "]:", "Handling message", msg.Uid)

		deleted := false
		flags := []string{}
		for _, flag := range msg.Flags {
			switch flag {
			case imap.DeletedFlag:
				deleted = true
				break
			case imap.SeenFlag:
				continue
			default:
				flags = append(flags, flag)
			}
		}
		if deleted {
			log.Println(c.config.Name, "[", c.config.state, "]:", "Ignoring message", msg.Uid)
			continue
		}

		log.Println(c.config.Name, "[", c.config.state, "]:", "Storing message", msg.Uid)

		body := msg.GetBody(section)
		err := c.imapconn.Append(update.Mailbox.Name, flags, msg.InternalDate, body)
		if err != nil {
			errors <- err
			return
		}

		deletes <- msg.Uid
	}

	close(deletes)
}

func (c *fetchSource) cleanMessages(deletes <-chan uint32, errors chan<- error) {
	seqset := new(imap.SeqSet)
	for uid := range deletes {
		log.Println(c.config.Name, "[", c.config.state, "]:", "Deleting message", uid)

		seqset.AddNum(uid)
	}

	if seqset.Empty() {
		close(errors)
		return
	}

	err := c.imapconn.UidStore(seqset, imap.AddFlags, []interface{}{imap.DeletedFlag}, nil)
	if err != nil {
		errors <- err
		return
	}

	close(errors)
}

func (c *fetchConfig) run(ctx context.Context) {
	c.err = c.start()
	if c.err == nil {
		c.err = c.watch(ctx)
	}
}
