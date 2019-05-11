package main

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	labels               = []string{"name"}
	accountState         = prometheus.NewDesc("mail_account_state", "State of mail accounts.", labels, nil)
	accountMessagesTotal = prometheus.NewDesc("mail_account_messages_total", "Number of processed messages.", labels, nil)
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
			accountState,
			prometheus.GaugeValue,
			float64(c.state),
			c.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			accountMessagesTotal,
			prometheus.CounterValue,
			float64(c.total),
			c.Name,
		)
	}
}
