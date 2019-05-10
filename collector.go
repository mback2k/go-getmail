package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	labels        = []string{"name"}
	account_state = prometheus.NewDesc("go_getmail_account_state", "State of go-getmail accounts.", labels, nil)
)

// Collector implements a prometheus.Collector.
type Collector struct {
	config *config
}

func NewCollector(config *config) *Collector {
	cc := &Collector{config: config}
	return cc
}

func (cc *Collector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cc, ch)
}

func (cc *Collector) Collect(ch chan<- prometheus.Metric) {
	for _, c := range cc.config.Accounts {
		ch <- prometheus.MustNewConstMetric(
			account_state,
			prometheus.GaugeValue,
			float64(c.state),
			c.Name,
		)
	}
}
