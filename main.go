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
	"runtime"
)

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	runtime.GC()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan *fetchConfig, 1)
	for _, c := range config.Accounts {
		go func(c *fetchConfig) {
			log.Println(c.Name, "[", c.state, "]:", c.Source.Server, "-->", c.Target.Server)
			c.run(ctx)
			done <- c
		}(c)
	}
	for range config.Accounts {
		c := <-done
		if c.err != nil {
			cancel()
			log.Println(c.Name, "[", c.state, "]:", c.err)
		}
	}
	for _, c := range config.Accounts {
		err := c.close()
		if err != nil {
			log.Println(c.Name, "[", c.state, "]:", err)
		}
	}
}
