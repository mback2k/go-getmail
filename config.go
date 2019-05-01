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

import "github.com/spf13/viper"

type configRollbar struct {
	AccessToken string
	Environment string
}

type config struct {
	Accounts []*fetchConfig

	Rollbar *configRollbar
}

func loadConfig() (*config, error) {
	vpr := viper.GetViper()
	vpr.SetConfigName("go-getmail")
	vpr.AddConfigPath("/etc/go-getmail/")
	vpr.AddConfigPath("$HOME/.go-getmail")
	vpr.AddConfigPath(".")
	err := vpr.ReadInConfig()
	if err != nil {
		return nil, err
	}

	var cfg config
	err = vpr.UnmarshalExact(&cfg)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
