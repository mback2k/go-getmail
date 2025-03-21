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
	"net/http"
	"runtime"

	"golang.org/x/sync/errgroup"

	"github.com/heroku/rollrus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rollbar/rollbar-go"
	"github.com/rollbar/rollbar-go/errors"

	log "github.com/sirupsen/logrus"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	if cfg.Logging != nil && cfg.Logging.Level != "" {
		l, err := log.ParseLevel(cfg.Logging.Level)
		if err != nil {
			log.Fatal(err)
		}
		log.SetLevel(l)
	}

	if cfg.Rollbar != nil && cfg.Rollbar.AccessToken != "" {
		rollbar.SetStackTracer(errors.StackTracer)
		rollrus.SetupLogging(cfg.Rollbar.AccessToken, cfg.Rollbar.Environment)
		defer rollrus.ReportPanic(cfg.Rollbar.AccessToken, cfg.Rollbar.Environment)
		log.Warn("Errors will be reported to rollbar.com!")
	}

	if cfg.Metrics != nil && cfg.Metrics.ListenAddress != "" {
		cc := NewCollector(cfg)
		prometheus.MustRegister(cc)
		http.Handle("/metrics", promhttp.Handler())
		go http.ListenAndServe(cfg.Metrics.ListenAddress, nil)
	}

	runtime.GC()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)
	for _, c := range cfg.Accounts {
		c.ctx = ctx
		c.log().Infof("%s --> %s", c.Source.Server, c.Target.Server)
		g.Go(c.run)
	}

	err = g.Wait()
	if err != nil {
		log.Error(err)
	}
}
