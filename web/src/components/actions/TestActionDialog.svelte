<script>
  import { Button, Input, Modal, Badge, toast } from '@chrissnell/chonky-ui';
  import { actionsApi } from '../../lib/actions/api.js';
  import { actionsStore } from '../../lib/actions/store.svelte.js';
  import { statusVariant, badArgKey } from '../../lib/actions/status.js';

  // `action` is the row whose Test button was clicked. Null while the
  // dialog is closed; the parent passes a fresh object on each open.
  let {
    open = $bindable(false),
    action = null,
    onClose,
  } = $props();

  // View state machine: 'input' → 'firing' → 'result'. 'input' renders
  // the per-arg fields; 'result' renders the response panel; 'firing'
  // grays out the Fire button while the request is in flight.
  let view = $state('input');
  let argValues = $state({});
  let argErrors = $state({});
  let result = $state(null);
  let topError = $state(null);

  let prevOpen = false;
  $effect(() => {
    if (open && !prevOpen) {
      reset();
    }
    prevOpen = open;
  });

  function reset() {
    view = 'input';
    argErrors = {};
    topError = null;
    result = null;
    const seed = {};
    if (action?.arg_schema) {
      for (const a of action.arg_schema) seed[a.key] = '';
    }
    argValues = seed;
  }

  function validateArg(key) {
    const spec = action?.arg_schema?.find((a) => a.key === key);
    if (!spec) return;
    const v = argValues[key] ?? '';
    if (!v) {
      // Required-ness is enforced server-side; we only soft-hint regex
      // failures client-side (per plan H4 step 1).
      argErrors = { ...argErrors, [key]: undefined };
      return;
    }
    const pattern = spec.regex || '^[A-Za-z0-9,_-]{1,32}$';
    try {
      const re = new RegExp(pattern);
      argErrors = { ...argErrors, [key]: re.test(v) ? undefined : `Doesn't match ${pattern}` };
    } catch {
      argErrors = { ...argErrors, [key]: undefined };
    }
  }

  async function fire() {
    if (!action?.id) return;
    topError = null;
    view = 'firing';
    try {
      const args = {};
      for (const [k, v] of Object.entries(argValues)) {
        if (v !== '' && v != null) args[k] = String(v);
      }
      const { data, error } = await actionsApi.testFire(action.id, args);
      if (error) {
        const detail = error?.error ?? error?.message ?? 'Test fire failed.';
        // The handler returns 400 with `{error: "bad arg: <key>"}` on
        // sanitization failure (sanitization runs before TestFire).
        // Outline the offending arg so the operator doesn't have to
        // re-read the banner to find what broke.
        const k = badArgKey(detail);
        if (k) {
          argErrors = { ...argErrors, [k]: detail };
          topError = null;
        } else {
          topError = detail;
        }
        view = 'input';
        return;
      }
      result = data;
      // bad_arg replies surface a per-key sanitization failure.
      // Outline the offending input so "Run again" doesn't strand the
      // operator without a hint.
      if (data?.status === 'bad_arg') {
        const k = badArgKey(data.reply_text);
        if (k) argErrors = { ...argErrors, [k]: data.reply_text };
      }
      view = 'result';
      // Refresh the invocation list so the test row appears live.
      actionsStore.refreshInvocations();
    } catch (e) {
      topError = e?.message ?? String(e);
      view = 'input';
    }
  }

  function runAgain() {
    view = 'input';
    result = null;
    topError = null;
  }

  function doClose() {
    open = false;
  }
</script>

<Modal bind:open onClose={() => onClose?.()} class="test-action-dialog">
  <Modal.Header>
    <h3 class="modal-title">Test "{action?.name ?? ''}"</h3>
    <Modal.Close aria-label="Close">x</Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <p class="subhead">
      OTP and sender-allowlist checks are bypassed because you are
      authenticated to the web UI. The full execution path runs and is
      logged.
    </p>

    {#if topError}
      <div class="error-banner" role="alert">{topError}</div>
    {/if}

    {#if view === 'result' && result}
      <div class="result">
        <div class="result-row">
          <span class="result-label">Status</span>
          <Badge variant={statusVariant(result.status)}>{result.status ?? 'unknown'}</Badge>
        </div>
        {#if result.exit_code != null}
          <div class="result-row">
            <span class="result-label">Exit code</span>
            <code class="value">{result.exit_code}</code>
          </div>
        {/if}
        {#if result.http_status != null}
          <div class="result-row">
            <span class="result-label">HTTP status</span>
            <code class="value">{result.http_status}</code>
          </div>
        {/if}
        {#if result.status_detail}
          <div class="result-row stacked">
            <span class="result-label">Detail</span>
            <pre class="block">{result.status_detail}</pre>
          </div>
        {/if}
        <div class="result-row stacked">
          <span class="result-label">Reply</span>
          <pre class="block reply" class:truncated={result.truncated}>{result.reply_text || '(no reply text)'}</pre>
          {#if result.truncated}
            <span class="hint">Trimmed to fit the 67-char APRS reply cap.</span>
          {/if}
        </div>
        {#if result.output_capture}
          <details class="output">
            <summary>Output capture</summary>
            <pre class="block">{result.output_capture}</pre>
          </details>
        {/if}
      </div>
    {:else}
      <div class="form">
        {#if !action?.arg_schema || action.arg_schema.length === 0}
          <p class="muted">This action accepts no args.</p>
        {:else}
          {#each action.arg_schema as spec (spec.key)}
            <div class="field">
              <label for={`test-arg-${spec.key}`}>
                {spec.key}
                {#if spec.required}<span class="req">*</span>{/if}
              </label>
              <Input
                id={`test-arg-${spec.key}`}
                type="text"
                value={argValues[spec.key] ?? ''}
                oninput={(e) => (argValues[spec.key] = e.target.value)}
                onblur={() => validateArg(spec.key)}
                placeholder={spec.regex || '^[A-Za-z0-9,_-]{1,32}$'}
                class={argErrors[spec.key] ? 'arg-invalid' : ''}
              />
              {#if argErrors[spec.key]}
                <p class="field-error">{argErrors[spec.key]}</p>
              {/if}
            </div>
          {/each}
        {/if}
      </div>
    {/if}
  </Modal.Body>
  <Modal.Footer>
    {#if view === 'result'}
      <Button variant="ghost" onclick={doClose}>Close</Button>
      <Button variant="primary" class="actions-solid" onclick={runAgain}>Run again</Button>
    {:else}
      <Button variant="ghost" onclick={doClose} disabled={view === 'firing'}>Cancel</Button>
      <Button variant="primary" class="actions-solid" onclick={fire} disabled={view === 'firing'}>
        {view === 'firing' ? 'Firing...' : 'Fire'}
      </Button>
    {/if}
  </Modal.Footer>
</Modal>

<style>
  .modal-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }
  .subhead {
    margin: 0 0 12px;
    padding: 8px 12px;
    background: var(--bg-tertiary, rgba(0, 0, 0, 0.04));
    border-left: 3px solid var(--accent, var(--color-primary));
    border-radius: 4px;
    font-size: 12px;
    color: var(--text-secondary, var(--color-text));
  }
  .error-banner {
    background: var(--color-danger-muted, rgba(220, 38, 38, 0.12));
    color: var(--color-danger, #b91c1c);
    border-left: 3px solid var(--color-danger, #b91c1c);
    padding: 8px 12px;
    border-radius: 4px;
    margin-bottom: 12px;
    font-size: 13px;
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .req {
    color: var(--color-danger, #b91c1c);
  }
  .field-error {
    margin: 0;
    font-size: 11px;
    color: var(--color-danger, #b91c1c);
  }
  .form :global(input) {
    margin: 0 !important;
  }
  .form :global(.arg-invalid) {
    border-color: var(--color-danger, #b91c1c) !important;
  }
  .muted {
    margin: 0;
    color: var(--color-text-muted, var(--text-muted));
    font-size: 13px;
  }
  .result {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .result-row {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .result-row.stacked {
    flex-direction: column;
    align-items: stretch;
    gap: 4px;
  }
  .result-label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .value {
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .block {
    margin: 0;
    padding: 8px 10px;
    background: var(--bg-tertiary, rgba(0, 0, 0, 0.04));
    border-radius: 4px;
    font-family: ui-monospace, monospace;
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 240px;
    overflow: auto;
  }
  .block.reply {
    border-left: 3px solid var(--color-primary, var(--accent));
  }
  .block.reply.truncated {
    border-left-color: var(--color-warning, #eab308);
  }
  .hint {
    font-size: 11px;
    color: var(--color-warning, #eab308);
  }
  .output summary {
    cursor: pointer;
    font-size: 12px;
    font-weight: 600;
    color: var(--color-text-dim, var(--text-muted));
    padding: 4px 0;
  }
  :global(.test-action-dialog) {
    max-width: 560px;
    width: calc(100% - 32px);
  }
</style>
