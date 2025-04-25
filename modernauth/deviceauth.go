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

package modernauth

import (
	"context"
	"sync"

	"golang.org/x/oauth2"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var providers = map[string]oauth2.Config{
	"microsoft": {
		ClientID: "9e5f94bc-e8a4-4e73-b8be-63364c29d753",
		Scopes:   []string{"https://outlook.office.com/IMAP.AccessAsUser.All", "offline_access"},
		Endpoint: oauth2.Endpoint{
			AuthURL:       "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			DeviceAuthURL: "https://login.microsoftonline.com/common/oauth2/v2.0/devicecode",
			TokenURL:      "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			AuthStyle:     oauth2.AuthStyleInParams,
		},
	},
}

type TokenSource struct {
	ctx  context.Context
	conf *oauth2.Config
	name string

	mqttopts *mqtt.ClientOptions
	mqttlock *sync.Mutex
}

func (ts *TokenSource) Token() (*oauth2.Token, error) {
	// Check if we have a token already
	token, err := ts.mqttLoadToken()
	if err != nil {
		return nil, err
	}

	// Check if we need to get a new token
	if token == nil {
		// If we don't have a token, we need to get one
		code, err := ts.conf.DeviceAuth(ts.ctx)
		if err != nil {
			return nil, err
		}

		// Forward the code to the user
		err = ts.mqttHassNotify(code)
		if err != nil {
			return nil, err
		}

		// Wait for the user to enter the code
		token, err = ts.conf.DeviceAccessToken(ts.ctx, code)
		if err != nil {
			return nil, err
		}
	}

	// Refresh the token if needed
	token, err = ts.conf.TokenSource(ts.ctx, token).Token()
	if err != nil {
		return nil, err
	}

	// Save the token
	err = ts.mqttSaveToken(token)
	if err != nil {
		return nil, err
	}

	return token, nil
}

func NewTokenSource(ctx context.Context, provider string, name string,
	mqttopts *mqtt.ClientOptions, mqttlock *sync.Mutex) oauth2.TokenSource {

	conf := providers[provider]
	return &TokenSource{ctx, &conf, name, mqttopts, mqttlock}
}
