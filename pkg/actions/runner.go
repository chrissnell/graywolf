package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	Logger   *slog.Logger
	Now      func() time.Time
}

// Runner owns one queue + worker per Action. Queues are created
// lazily on first Submit.
type Runner struct {
	cfg    RunnerConfig
	now    func() time.Time
	logger *slog.Logger

	mu     sync.Mutex
	queues map[uint]*actionQueue
	closed bool
}

// actionQueue serializes submits + the channel push under mu so that
// Stop can race with Submit without panicking on a closed channel.
type actionQueue struct {
	ch       chan workItem
	mu       sync.Mutex
	lastFire time.Time
	closed   bool
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
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{cfg: cfg, now: cfg.Now, logger: logger, queues: map[uint]*actionQueue{}}
}

// Stop drains state and closes every per-Action queue. Once Stop
// returns, no further Submit will enqueue or spawn goroutines for new
// queues; in-flight worker goroutines drain naturally as their
// channels close.
func (r *Runner) Stop() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	queues := make([]*actionQueue, 0, len(r.queues))
	for _, q := range r.queues {
		queues = append(queues, q)
	}
	r.mu.Unlock()

	for _, q := range queues {
		q.mu.Lock()
		if !q.closed {
			q.closed = true
			close(q.ch)
		}
		q.mu.Unlock()
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
		// Either queue_depth == 0 (run inline) or runner already
		// stopped between the closed check above and queueFor.
		if a.QueueDepth > 0 {
			return
		}
		go r.runOne(ctx, workItem{ctx: ctx, inv: inv, action: a, channel: channel})
		return
	}

	// Hold q.mu across the rate-limit check, the lastFire reservation,
	// and the channel send so Stop() can close ch safely after seeing
	// q.closed.
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	prev := q.lastFire
	if a.RateLimitSec > 0 && !prev.IsZero() && r.now().Sub(prev) < time.Duration(a.RateLimitSec)*time.Second {
		q.mu.Unlock()
		r.replyAndAudit(ctx, inv, channel, Result{Status: StatusRateLimited})
		return
	}
	if a.RateLimitSec > 0 {
		q.lastFire = r.now()
	}
	select {
	case q.ch <- workItem{ctx: ctx, inv: inv, action: a, channel: channel}:
		q.mu.Unlock()
	default:
		// Roll the rate-limit window back: a busy-rejected submit
		// didn't actually consume the slot, so the next legitimate
		// submit shouldn't be silently rate-limited by it.
		if a.RateLimitSec > 0 {
			q.lastFire = prev
		}
		q.mu.Unlock()
		r.replyAndAudit(ctx, inv, channel, Result{Status: StatusBusy})
	}
}

func (r *Runner) queueFor(a *configstore.Action) *actionQueue {
	if a.QueueDepth <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
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
	res := r.executeWithRecover(ctx, exe, ExecRequest{
		Action: it.action, Invocation: it.inv, Timeout: timeout,
	})
	r.replyAndAudit(ctx, it.inv, it.channel, res)
}

// executeWithRecover invokes the executor with a panic guard so a
// buggy third-party Executor cannot kill the per-Action worker
// goroutine and freeze the queue. Panics map to StatusError with a
// safe detail and are logged with the action name. Built-in
// executors (command, webhook) do not panic; this guard exists for
// the registry's extension surface.
func (r *Runner) executeWithRecover(ctx context.Context, exe Executor, req ExecRequest) (res Result) {
	defer func() {
		if rec := recover(); rec != nil {
			r.logger.Error("actions: executor panic",
				"action", req.Action.Name, "type", req.Action.Type, "panic", rec)
			res = Result{Status: StatusError, StatusDetail: "panic"}
		}
	}()
	return exe.Execute(ctx, req)
}

// Reply dispatches a synthetic reply + audit row without queueing
// real work. Used by the classifier short-circuit paths
// (denied, bad_otp, bad_arg, no_credential) so those outcomes still
// flow through the normal reply + audit pipeline without exercising
// the per-Action queue or executor.
func (r *Runner) Reply(ctx context.Context, inv Invocation, channel uint32, res Result) {
	r.replyAndAudit(ctx, inv, channel, res)
}

func (r *Runner) replyAndAudit(ctx context.Context, inv Invocation, channel uint32, res Result) {
	text, truncated := FormatReply(res)
	if r.cfg.Replies != nil {
		if err := r.cfg.Replies.SendReply(ctx, channel, inv.Source, inv.SenderCall, text); err != nil {
			r.logger.Error("actions: reply send failed",
				"action", inv.ActionName,
				"sender", inv.SenderCall,
				"status", string(res.Status),
				"err", err)
		}
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
		if inv.OTPCredentialID != 0 {
			id := inv.OTPCredentialID
			row.OTPCredentialID = &id
		}
		if err := r.cfg.Audit.Insert(ctx, row); err != nil {
			r.logger.Error("actions: audit insert failed",
				"action", inv.ActionName,
				"sender", inv.SenderCall,
				"status", string(res.Status),
				"err", err)
		}
	}
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
