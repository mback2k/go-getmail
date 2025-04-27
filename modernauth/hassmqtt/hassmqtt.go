/*
	go-getmail - Retrieve and forward e-mails between IMAP servers.
	Copyright (C) 2025  Marc Hoersken <info@marc-hoersken.de>

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

package hassmqtt

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type HassMqttAuthBackend struct {
	ctx  context.Context
	name string

	mqttopts *mqtt.ClientOptions
	mqttlock *sync.Mutex
}

func (ab *HassMqttAuthBackend) uniqueID() string {
	return strings.ReplaceAll(strings.ReplaceAll(ab.name, "@", "-"), ".", "-")
}

func (ab *HassMqttAuthBackend) LoadToken() (*oauth2.Token, error) {
	unique_id := ab.uniqueID()
	base := "modernauth/" + ab.mqttopts.ClientID + "/" + unique_id

	ab.mqttlock.Lock()
	defer ab.mqttlock.Unlock()

	client := mqtt.NewClient(ab.mqttopts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	defer client.Disconnect(250)

	errors := make(chan error)
	tokens := make(chan *oauth2.Token)
	token = client.Subscribe(base+"/token", 0, func(client mqtt.Client, msg mqtt.Message) {
		tok := &oauth2.Token{}
		err := json.Unmarshal(msg.Payload(), tok)
		if err != nil {
			errors <- err
		} else {
			tokens <- tok
		}
	})
	if token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	select {
	case err := <-errors:
		return nil, err
	case tok := <-tokens:
		return tok, nil
	case <-ab.ctx.Done():
	case <-time.After(time.Second):
	}

	return nil, nil
}

func (ab *HassMqttAuthBackend) SaveToken(tok *oauth2.Token) error {
	unique_id := ab.uniqueID()
	base := "modernauth/" + ab.mqttopts.ClientID + "/" + unique_id

	ab.mqttlock.Lock()
	defer ab.mqttlock.Unlock()

	client := mqtt.NewClient(ab.mqttopts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	defer client.Disconnect(250)

	bytes, err := json.Marshal(tok)
	if err != nil {
		return err
	}

	token = client.Publish(base+"/token", 0, true, bytes)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	return nil
}

func (ab *HassMqttAuthBackend) Notify(code *oauth2.DeviceAuthResponse) error {
	unique_id := ab.uniqueID()
	base := "homeassistant/event/" + ab.mqttopts.ClientID + "/" + unique_id

	ab.mqttlock.Lock()
	defer ab.mqttlock.Unlock()

	client := mqtt.NewClient(ab.mqttopts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	defer client.Disconnect(250)

	config := map[string]interface{}{
		"~":           base,
		"name":        ab.name,
		"event_types": []string{"auth"},
		"state_topic": "~/state",
		"unique_id":   ab.mqttopts.ClientID + "-" + unique_id,
		"device": map[string]interface{}{
			"identifiers": []string{ab.mqttopts.ClientID},
			"name":        ab.name,
		},
	}
	bytes, err := json.Marshal(config)
	if err != nil {
		return err
	}

	token = client.Publish(base+"/config", 0, false, bytes)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	event := map[string]interface{}{
		"event_type": "auth",
		"link":       code.VerificationURI,
		"code":       code.UserCode,
	}
	bytes, err = json.Marshal(event)
	if err != nil {
		return err
	}

	token = client.Publish(base+"/state", 0, false, bytes)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}

	return nil
}

func NewHassMqttAuthBackend(ctx context.Context, name string,
	mqttopts *mqtt.ClientOptions, mqttlock *sync.Mutex) *HassMqttAuthBackend {

	return &HassMqttAuthBackend{
		ctx:  ctx,
		name: name,

		mqttopts: mqttopts,
		mqttlock: mqttlock,
	}
}
