package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// AuditSink writes invocation rows. Wraps the configstore repo for
// testability.
type AuditSink interface {
	Insert(ctx context.Context, row *configstore.ActionInvocation) error
}

type RunnerConfig struct {
	Registry *ExecutorRegistry
	Replies  ReplySender
	Audit    AuditSink
	Now      func() time.Time
}

// Runner owns one queue + worker per Action. Queues are created
// lazily on first Submit.
type Runner struct {
	cfg RunnerConfig
	now func() time.Time

	mu     sync.Mutex
	queues map[uint]*actionQueue
	closed bool
}

type actionQueue struct {
	ch       chan workItem
	mu       sync.Mutex
	lastFire time.Time
}

type workItem struct {
	ctx     context.Context
	inv     Invocation
	action  *configstore.Action
	channel uint32
}

func NewRunner(cfg RunnerConfig) *Runner {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Runner{cfg: cfg, now: cfg.Now, queues: map[uint]*actionQueue{}}
}

func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for _, q := range r.queues {
		close(q.ch)
	}
}

// Submit queues one invocation for processing. The reply is dispatched
// asynchronously; Submit returns immediately. Disabled / no-credential
// / busy / rate-limited paths reply + audit synchronously inside
// Submit so the caller's request lifetime captures the full path.
func (r *Runner) Submit(ctx context.Context, inv Invocation, a *configstore.Action, channel uint32) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return
	}
	if a == nil {
		r.replyAndAudit(ctx, inv, channel, Result{Status: StatusUnknown})
		return
	}
	if !a.Enabled {
		r.replyAndAudit(ctx, inv, channel, Result{Status: StatusDisabled})
		return
	}
	if a.OTPRequired && a.OTPCredentialID == nil {
		r.replyAndAudit(ctx, inv, channel, Result{Status: StatusNoCredential})
		return
	}

	q := r.queueFor(a)
	if q == nil {
		// queue_depth == 0: bypass queue, run inline in a goroutine.
		go r.runOne(ctx, workItem{ctx: ctx, inv: inv, action: a, channel: channel})
		return
	}

	q.mu.Lock()
	if a.RateLimitSec > 0 && !q.lastFire.IsZero() && r.now().Sub(q.lastFire) < time.Duration(a.RateLimitSec)*time.Second {
		q.mu.Unlock()
		r.replyAndAudit(ctx, inv, channel, Result{Status: StatusRateLimited})
		return
	}
	// Reserve the slot now: the rate-limit window must close even if
	// the worker hasn't drained the previous item yet, otherwise rapid
	// back-to-back submits all slip through with lastFire still zero.
	if a.RateLimitSec > 0 {
		q.lastFire = r.now()
	}
	q.mu.Unlock()

	select {
	case q.ch <- workItem{ctx: ctx, inv: inv, action: a, channel: channel}:
	default:
		r.replyAndAudit(ctx, inv, channel, Result{Status: StatusBusy})
	}
}

func (r *Runner) queueFor(a *configstore.Action) *actionQueue {
	if a.QueueDepth <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if q, ok := r.queues[a.ID]; ok {
		return q
	}
	q := &actionQueue{ch: make(chan workItem, a.QueueDepth)}
	r.queues[a.ID] = q
	go r.workerLoop(q)
	return q
}

func (r *Runner) workerLoop(q *actionQueue) {
	for it := range q.ch {
		r.runOne(it.ctx, it)
	}
}

func (r *Runner) runOne(ctx context.Context, it workItem) {
	exe, ok := r.cfg.Registry.Lookup(it.action.Type)
	if !ok {
		r.replyAndAudit(ctx, it.inv, it.channel, Result{
			Status: StatusError, StatusDetail: fmt.Sprintf("no executor for type %q", it.action.Type),
		})
		return
	}
	timeout := time.Duration(it.action.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	res := exe.Execute(ctx, ExecRequest{
		Action: it.action, Invocation: it.inv, Timeout: timeout,
	})
	r.replyAndAudit(ctx, it.inv, it.channel, res)
}

func (r *Runner) replyAndAudit(ctx context.Context, inv Invocation, channel uint32, res Result) {
	text := FormatReply(res)
	truncated := len(res.OutputCapture) > 0 && !endsWithSnippet(text, res.OutputCapture)
	if r.cfg.Replies != nil {
		_ = r.cfg.Replies.SendReply(ctx, channel, inv.Source, inv.SenderCall, text)
	}
	if r.cfg.Audit != nil {
		row := &configstore.ActionInvocation{
			ActionNameAt:  inv.ActionName,
			SenderCall:    inv.SenderCall,
			Source:        string(inv.Source),
			OTPVerified:   inv.OTPVerified,
			RawArgsJSON:   marshalArgs(inv.Args),
			Status:        string(res.Status),
			StatusDetail:  res.StatusDetail,
			ExitCode:      res.ExitCode,
			HTTPStatus:    res.HTTPStatus,
			OutputCapture: res.OutputCapture,
			ReplyText:     text,
			Truncated:     truncated,
			CreatedAt:     r.now(),
		}
		if inv.ActionID != 0 {
			id := inv.ActionID
			row.ActionID = &id
		}
		_ = r.cfg.Audit.Insert(ctx, row)
	}
}

func endsWithSnippet(big, small string) bool {
	if len(small) > 50 {
		small = small[:50]
	}
	return len(big) >= len(small) && big[len(big)-len(small):] == small
}

func marshalArgs(kvs []KeyValue) string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		v := kv.Value
		if len(v) > 64 {
			v = v[:64]
		}
		m[kv.Key] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}
