package homedrive

import (
	"context"
	"errors"
	"time"

	"github.com/asnowfix/home-automation/myhome/alert"
	"github.com/go-logr/logr"
)

// EvalEvery is the evaluation period (design section 3: every evaluator tick, 30 s).
const EvalEvery = 30 * time.Second

// Producer evaluates AlertRules over the cache and hands the observations to the shared alert core
// (#845), which owns `for`, the resolve hold, the startup grace, persistence and publishing on
// <instance>/alert/<rule>/<key> (#843). It knows nothing about channels: delivery (#714) subscribes.
type Producer struct {
	log   logr.Logger
	cache *Cache
	ev    *alert.Evaluator
	rules []AlertRule
	now   func() time.Time
}

// NewProducer builds a Producer. now defaults to time.Now.
func NewProducer(log logr.Logger, cache *Cache, ev *alert.Evaluator, now func() time.Time) *Producer {
	if now == nil {
		now = time.Now
	}
	return &Producer{log: log.WithName("homedrive-alert"), cache: cache, ev: ev, rules: AlertRules(), now: now}
}

// Evaluate runs every rule once over the current snapshot.
func (p *Producer) Evaluate(ctx context.Context) {
	now := p.now()
	views := p.cache.Snapshot(now)
	for _, r := range p.rules {
		obs := make([]alert.Observation, 0, len(views))
		for _, v := range views {
			obs = append(obs, observe(r, v, now))
		}
		if err := p.ev.Eval(ctx, r.Rule, obs); err != nil {
			// Only contract violations come back: a bug in a rule, logged and not retried.
			p.log.Error(err, "homedrive alert rule violated the alert contract", "rule", r.Name, "contract", errors.Is(err, alert.ErrContract))
		}
	}
}

func observe(r AlertRule, v View, now time.Time) alert.Observation {
	key := Key{v.Host, v.User}
	o := alert.Observation{
		Key:    alertKey(key),
		Labels: map[string]string{"subject": truncate("homedrive:"+v.Host+"/"+v.User, alert.MaxLabelLength)},
	}
	if r.Content && !trusted(v) {
		o.Frozen = true
		return o
	}
	rd := r.Eval(v, now)
	o.Active = rd.Active
	o.Summary = summaryFor(rd.Noun, key)
	o.Detail = rd.Detail
	if !rd.Active {
		o.Detail = r.Cleared
	}
	return o
}

// Run evaluates every EvalEvery until ctx ends. Start it after mc.Start() (alert wiring rule).
func (p *Producer) Run(ctx context.Context) {
	t := time.NewTicker(EvalEvery)
	defer t.Stop()
	for {
		p.Evaluate(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
