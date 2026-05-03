<script>
  import { Button, Input, Select, Toggle, RadioGroup, Radio, Modal, toast } from '@chrissnell/chonky-ui';
  import { actionsStore } from '../../lib/actions/store.svelte.js';
  import { actionsApi } from '../../lib/actions/api.js';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import ArgSchemaEditor from './ArgSchemaEditor.svelte';
  import SenderAllowlistEditor from './SenderAllowlistEditor.svelte';
  import HeadersEditor from './HeadersEditor.svelte';

  // `action` is null when creating, or a fully-populated dto.Action
  // copy when editing. The parent passes a fresh object on each open
  // so cancel discards the in-progress edits.
  let {
    open = $bindable(false),
    action = null,
    onClose,
    onManageCredentials,
  } = $props();

  // Operator-facing name regex: letters, digits, dot, dash, underscore.
  // Mirrors the backend invariant in pkg/webapi/actions.go validateAction.
  const NAME_RE = /^[A-Za-z0-9._-]+$/;

  function emptyForm() {
    return {
      id: 0,
      name: '',
      description: '',
      type: 'command',
      command_path: '',
      working_dir: '',
      webhook_method: 'GET',
      webhook_url: '',
      webhook_headers: {},
      webhook_body_template: '',
      timeout_sec: 10,
      otp_required: true,
      otp_credential_id: null,
      sender_allowlist: '',
      arg_schema: [],
      rate_limit_sec: 5,
      queue_depth: 8,
      enabled: true,
    };
  }

  let form = $state(emptyForm());
  let nameError = $state(null);
  let topError = $state(null);
  let fieldErrors = $state({});
  let saving = $state(false);
  let confirmDeleteOpen = $state(false);

  let argEditor = $state(null);
  let headersEditor = $state(null);

  // Reset the form whenever the modal transitions from closed → open or
  // the bound `action` changes. Doing it in $effect keeps state-on-open
  // consistent without leaking edits between sessions.
  // Backend serializes ArgSpec.regex / required with `omitempty`, so a
  // saved row like `{key: "foo"}` arrives with both fields undefined.
  // Chonky `Input` and `Toggle` reject `undefined` for a bindable that
  // has a default, so normalize before binding to avoid props_invalid_value.
  function normalizeArgSchema(rows) {
    if (!Array.isArray(rows)) return [];
    return rows.map((r) => ({
      key: r?.key ?? '',
      regex: r?.regex ?? '',
      required: !!r?.required,
    }));
  }

  let prevOpen = false;
  $effect(() => {
    if (open && !prevOpen) {
      form = action ? { ...emptyForm(), ...structuredClone($state.snapshot(action)) } : emptyForm();
      form.arg_schema = normalizeArgSchema(form.arg_schema);
      if (!form.webhook_headers) form.webhook_headers = {};
      // The backend may send blank webhook_method on a brand-new action;
      // default it so the Select doesn't render empty when the operator
      // toggles type to webhook.
      if (form.type === 'webhook' && !form.webhook_method) form.webhook_method = 'GET';
      nameError = null;
      topError = null;
      fieldErrors = {};
    }
    prevOpen = open;
  });

  // Defaulting webhook_method is also needed when the operator switches
  // type from command to webhook on an existing action: the saved
  // webhook_method is blank because we strip it in buildBody for command
  // type. Watch the type field and seed a sensible default.
  $effect(() => {
    if (form.type === 'webhook' && !form.webhook_method) {
      form.webhook_method = 'GET';
    }
  });

  let isEdit = $derived(!!action?.id);
  let title = $derived(isEdit ? `Edit Action: ${action.name}` : 'New Action');

  let credOptions = $derived([
    { value: '', label: '(select credential)' },
    ...actionsStore.creds.map((c) => ({ value: String(c.id), label: c.name })),
  ]);

  // Select binds against strings; the wire value is a number (or null).
  // Mirror both directions so the dropdown reflects the bound action
  // and edits propagate back as numbers.
  let credSelectValue = $state('');
  $effect(() => {
    credSelectValue = form.otp_credential_id ? String(form.otp_credential_id) : '';
  });
  function onCredChange(v) {
    form.otp_credential_id = v ? Number(v) : null;
  }

  let methodOptions = [
    { value: 'GET', label: 'GET' },
    { value: 'POST', label: 'POST' },
  ];

  function validateName() {
    const v = form.name.trim();
    if (!v) {
      nameError = 'Name is required.';
      return false;
    }
    if (!NAME_RE.test(v)) {
      nameError = 'Letters, digits, dot, dash, underscore only.';
      return false;
    }
    nameError = null;
    return true;
  }

  function buildBody() {
    // Strip wire fields the API treats as derived/read-only and
    // empty-out the inactive type's sub-block so PUT doesn't carry
    // stale values across a type switch.
    const out = { ...form };
    delete out.id;
    delete out.last_invoked_at;
    delete out.last_invoked_by;
    out.name = out.name.trim();
    out.description = out.description.trim();
    if (out.type === 'command') {
      out.webhook_method = '';
      out.webhook_url = '';
      out.webhook_headers = {};
      out.webhook_body_template = '';
    } else {
      out.command_path = '';
      out.working_dir = '';
      if (out.webhook_method !== 'POST') out.webhook_body_template = '';
    }
    if (!out.otp_required) out.otp_credential_id = null;
    else out.otp_credential_id = out.otp_credential_id ? Number(out.otp_credential_id) : null;
    out.timeout_sec = Number(out.timeout_sec) || 10;
    out.rate_limit_sec = Number(out.rate_limit_sec) || 0;
    out.queue_depth = Number(out.queue_depth) || 0;
    return out;
  }

  function applyServerError(err) {
    const msg = err?.error ?? err?.message ?? 'Save failed.';
    // The webapi handlers return errors in `{error: "field: detail"}`
    // shape. Indexed list prefixes like `arg_schema[0]` get stripped to
    // their root field so the matching input gets a red outline; unknown
    // shapes fall through to the top-of-modal banner.
    if (typeof msg === 'string' && msg.includes(':')) {
      const idx = msg.indexOf(':');
      const raw = msg.slice(0, idx).trim();
      const detail = msg.slice(idx + 1).trim();
      const field = raw.replace(/\[\d+\]$/, '');
      const known = [
        'name', 'description', 'type', 'command_path', 'working_dir',
        'webhook_method', 'webhook_url', 'webhook_body_template',
        'timeout_sec', 'otp_required', 'otp_credential_id',
        'sender_allowlist', 'arg_schema', 'rate_limit_sec', 'queue_depth',
      ];
      if (known.includes(field)) {
        // Preserve the original index in the error text so the operator
        // knows which row is broken (the editor doesn't yet outline
        // individual rows from server-side errors).
        fieldErrors = { ...fieldErrors, [field]: raw === field ? detail : `${raw}: ${detail}` };
        topError = null;
        return;
      }
    }
    topError = msg;
  }

  async function save() {
    fieldErrors = {};
    topError = null;
    if (!validateName()) return;
    if (argEditor?.hasErrors()) {
      topError = 'One or more arg-schema regexes are invalid.';
      return;
    }
    if (headersEditor?.hasErrors()) {
      topError = 'One or more header names are invalid.';
      return;
    }
    if (form.otp_required && !form.otp_credential_id) {
      fieldErrors = { ...fieldErrors, otp_credential_id: 'Required when OTP is enabled.' };
      return;
    }
    saving = true;
    try {
      const body = buildBody();
      const { error } = isEdit
        ? await actionsApi.update(action.id, body)
        : await actionsApi.create(body);
      if (error) {
        applyServerError(error);
        return;
      }
      toast(isEdit ? `Updated action "${body.name}".` : `Created action "${body.name}".`, 'success');
      await actionsStore.loadAll();
      doClose();
    } catch (e) {
      topError = e?.message ?? String(e);
    } finally {
      saving = false;
    }
  }

  async function confirmDelete() {
    if (!action?.id) return;
    const { error } = await actionsApi.remove(action.id);
    if (error) {
      toast(`Delete failed: ${error.error ?? error.message ?? error}`, 'error');
      return;
    }
    toast(`Deleted action "${action.name}".`, 'success');
    await actionsStore.loadAll();
    doClose();
  }

  function doClose() {
    // Setting open=false triggers chonky's Modal onClose, which calls
    // the parent's onClose. Don't double-fire it here.
    open = false;
  }
</script>

<Modal bind:open onClose={() => onClose?.()} class="action-edit-modal">
  <Modal.Header>
    <h3 class="modal-title">{title}</h3>
    <Modal.Close aria-label="Close">x</Modal.Close>
  </Modal.Header>
  <Modal.Body>
    {#if topError}
      <div class="error-banner" role="alert">{topError}</div>
    {/if}

    <div class="form">
      <div class="field">
        <label for="action-name">Name <span class="req">*</span></label>
        <Input
          id="action-name"
          type="text"
          bind:value={form.name}
          onblur={validateName}
          class={nameError || fieldErrors.name ? 'field-invalid' : ''}
        />
        <p class="hint">Used as keyword in the message. Letters, digits, dot, dash, underscore.</p>
        {#if nameError}<p class="field-error">{nameError}</p>{/if}
        {#if fieldErrors.name}<p class="field-error">{fieldErrors.name}</p>{/if}
      </div>

      <div class="field">
        <label for="action-desc">Description</label>
        <textarea
          id="action-desc"
          class="textarea"
          rows="2"
          bind:value={form.description}
        ></textarea>
      </div>

      <div class="field">
        <span class="label">Type <span class="req">*</span></span>
        <RadioGroup bind:value={form.type}>
          <div class="radio-row">
            <Radio value="command" label="Command" />
            <Radio value="webhook" label="Webhook" />
          </div>
        </RadioGroup>
      </div>

      {#if form.type === 'command'}
        <div class="field">
          <label for="action-cmd">Command path <span class="req">*</span></label>
          <Input
            id="action-cmd"
            type="text"
            placeholder="/usr/local/bin/turn-on-lights"
            bind:value={form.command_path}
            class={fieldErrors.command_path ? 'field-invalid' : ''}
          />
          <p class="hint">Absolute path to the executable. Validated when you save.</p>
          {#if fieldErrors.command_path}<p class="field-error">{fieldErrors.command_path}</p>{/if}

          <details class="exec-help">
            <summary>How your command is invoked</summary>
            <div class="exec-help-body">
              <pre class="exec-cli">&lt;command&gt; &lt;action name&gt; &lt;sender callsign&gt; &lt;otp verified&gt; [&lt;key=value&gt; &lt;key=value&gt; ...]</pre>

              <h4>Arguments (positional)</h4>
              <dl>
                <dt><code>$1</code></dt><dd>action name</dd>
                <dt><code>$2</code></dt><dd>sender callsign</dd>
                <dt><code>$3</code></dt><dd><code>true</code> or <code>false</code> -- whether the TOTP code validated</dd>
                <dt><code>$4+</code></dt><dd>one <code>key=value</code> per arg-schema entry, in the order the sender supplied them</dd>
              </dl>

              <h4>Environment</h4>
              <dl>
                <dt><code>GW_ACTION_NAME</code></dt><dd>same as <code>$1</code></dd>
                <dt><code>GW_SENDER_CALL</code></dt><dd>same as <code>$2</code></dd>
                <dt><code>GW_OTP_VERIFIED</code></dt><dd>same as <code>$3</code></dd>
                <dt><code>GW_OTP_CRED_NAME</code></dt><dd>name of the credential that validated the code (empty if OTP not required)</dd>
                <dt><code>GW_SOURCE</code></dt><dd>where the trigger arrived from (e.g. <code>aprs</code>, <code>test</code>)</dd>
                <dt><code>GW_INVOCATION_ID</code></dt><dd>numeric id of the audit row for this run</dd>
                <dt><code>GW_ARG_&lt;KEY&gt;</code></dt><dd>one per arg, key uppercased, non-alphanumerics turned into <code>_</code></dd>
                <dt><code>PATH</code></dt><dd>preset to <code>/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin</code>. The parent process environment is <strong>not</strong> inherited.</dd>
              </dl>

              <h4>Runtime</h4>
              <ul>
                <li>Working directory: the value above, otherwise the directory containing the command.</li>
                <li>Timeout: the value above. SIGTERM on expiry, SIGKILL 2 seconds later.</li>
                <li>Output: stdout and stderr are merged; the first 4 KiB are captured and the leading characters are echoed back to the sender as the APRS reply.</li>
              </ul>
            </div>
          </details>
        </div>

        <div class="field">
          <label for="action-wd">Working directory</label>
          <Input
            id="action-wd"
            type="text"
            placeholder="directory of command path"
            bind:value={form.working_dir}
          />
        </div>

        <div class="field narrow">
          <label for="action-timeout">Timeout (s)</label>
          <Input
            id="action-timeout"
            type="number"
            min="1"
            max="60"
            bind:value={form.timeout_sec}
          />
        </div>
      {:else}
        <div class="field narrow">
          <label for="action-method">Method</label>
          <Select
            id="action-method"
            bind:value={form.webhook_method}
            options={methodOptions}
          />
        </div>

        <div class="field">
          <label for="action-url">URL <span class="req">*</span></label>
          <Input
            id="action-url"
            type="text"
            placeholder="https://example.com/hook"
            bind:value={form.webhook_url}
            class={fieldErrors.webhook_url ? 'field-invalid' : ''}
          />
          {#if fieldErrors.webhook_url}<p class="field-error">{fieldErrors.webhook_url}</p>{/if}
        </div>

        <div class="field">
          <span class="label">Headers</span>
          <HeadersEditor bind:headers={form.webhook_headers} bind:this={headersEditor} />
        </div>

        {#if form.webhook_method === 'POST'}
          <div class="field">
            <label for="action-body">Body template</label>
            <textarea
              id="action-body"
              class="textarea code"
              rows="4"
              placeholder="Default: form-encoded args. Use {`{{arg.key}}`} for token expansion."
              bind:value={form.webhook_body_template}
            ></textarea>
          </div>
        {/if}

        <div class="field narrow">
          <label for="action-wh-timeout">Timeout (s)</label>
          <Input
            id="action-wh-timeout"
            type="number"
            min="1"
            max="60"
            bind:value={form.timeout_sec}
          />
        </div>
      {/if}

      <div class="field">
        <span class="label">OTP</span>
        <Toggle bind:checked={form.otp_required} label="Require valid one-time code" />
        {#if form.otp_required}
          <div class="otp-cred">
            <Select
              bind:value={credSelectValue}
              options={credOptions}
              onValueChange={onCredChange}
              class={fieldErrors.otp_credential_id ? 'field-invalid' : ''}
            />
            <button
              type="button"
              class="link"
              onclick={() => onManageCredentials?.()}
            >+ Manage credentials</button>
          </div>
          {#if fieldErrors.otp_credential_id}
            <p class="field-error">{fieldErrors.otp_credential_id}</p>
          {/if}
        {/if}
      </div>

      <div class="field">
        <label for="action-allowlist">Sender allowlist</label>
        <SenderAllowlistEditor bind:value={form.sender_allowlist} />
        <p class="hint">Comma-separated callsigns or <code>CALL-*</code> wildcards. Empty = anyone (OTP still applies).</p>
      </div>

      <div class="field">
        <span class="label">Allowed args</span>
        <ArgSchemaEditor bind:argSchema={form.arg_schema} bind:this={argEditor} />
      </div>

      <div class="field narrow">
        <label for="action-rate">Rate limit (s)</label>
        <Input
          id="action-rate"
          type="number"
          min="0"
          bind:value={form.rate_limit_sec}
        />
        <p class="hint">Seconds between consecutive invocations.</p>
      </div>

      <div class="field narrow">
        <label for="action-queue">Queue depth</label>
        <Input
          id="action-queue"
          type="number"
          min="0"
          max="32"
          bind:value={form.queue_depth}
        />
        <p class="hint">
          When an Action is already running and another invocation arrives, this
          many requests can wait in line. Beyond that the sender gets a
          <code>busy</code> reply. Set to 0 to allow parallel runs (use only for
          read-only commands).
        </p>
      </div>

      <div class="field">
        <span class="label">Status</span>
        <Toggle bind:checked={form.enabled} label="Action is enabled" />
        <p class="hint">Disabled Actions still appear here but never fire; senders get a <code>disabled</code> reply.</p>
      </div>

      <div class="field">
        <span class="label">Reply policy</span>
        <p class="readonly-summary">
          Always replies; reply may include the first ~50 chars of stdout/response.
        </p>
      </div>
    </div>
  </Modal.Body>
  <Modal.Footer>
    <Button variant="ghost" onclick={doClose} disabled={saving}>Cancel</Button>
    {#if isEdit}
      <Button variant="danger" class="actions-solid" onclick={() => (confirmDeleteOpen = true)} disabled={saving}>
        Delete
      </Button>
    {/if}
    <Button variant="primary" class="actions-solid" onclick={save} disabled={saving}>
      {saving ? 'Saving...' : 'Save changes'}
    </Button>
  </Modal.Footer>
</Modal>

<ConfirmDialog
  bind:open={confirmDeleteOpen}
  title="Delete action?"
  message={action ? `Permanently delete "${action.name}"? This cannot be undone.` : ''}
  confirmLabel="Delete"
  onConfirm={confirmDelete}
/>

<style>
  .modal-title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }
  .form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field.narrow {
    max-width: 220px;
  }
  .field label,
  .field .label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    color: var(--color-text-dim, var(--text-muted));
  }
  .req {
    color: var(--color-danger, #b91c1c);
  }
  .hint {
    margin: 0;
    font-size: 11px;
    color: var(--color-text-muted, var(--text-muted));
  }
  .hint code {
    font-family: ui-monospace, monospace;
    background: var(--accent-bg, rgba(0, 0, 0, 0.05));
    padding: 1px 4px;
    border-radius: 3px;
  }
  .field-error {
    margin: 0;
    font-size: 11px;
    color: var(--color-danger, #b91c1c);
  }
  .exec-help {
    margin-top: 4px;
    border: 1px solid var(--color-border, var(--border));
    border-radius: var(--radius, 4px);
    background: var(--bg-tertiary, rgba(0, 0, 0, 0.02));
    font-size: 12px;
  }
  .exec-help[open] > summary {
    border-bottom: 1px solid var(--color-border, var(--border));
  }
  .exec-help > summary {
    cursor: pointer;
    padding: 6px 10px;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--color-text-dim, var(--text-muted));
    list-style: none;
    user-select: none;
  }
  .exec-help > summary::-webkit-details-marker {
    display: none;
  }
  .exec-help > summary::before {
    content: '▸';
    display: inline-block;
    width: 1em;
    margin-right: 4px;
    transition: transform 120ms ease;
  }
  .exec-help[open] > summary::before {
    transform: rotate(90deg);
  }
  .exec-help-body {
    padding: 8px 12px 10px;
    color: var(--text-secondary);
    line-height: 1.5;
  }
  .exec-help-body h4 {
    margin: 10px 0 4px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    color: var(--color-text-dim, var(--text-muted));
  }
  .exec-help-body h4:first-of-type {
    margin-top: 12px;
  }
  .exec-help-body code {
    font-family: 'SauceCodePro Nerd Font', ui-monospace, monospace;
    background: var(--accent-bg, rgba(0, 0, 0, 0.05));
    color: var(--text-primary);
    padding: 1px 4px;
    border-radius: 3px;
    font-size: 11.5px;
  }
  .exec-cli {
    margin: 0;
    padding: 8px 10px;
    background: var(--bg-secondary, rgba(0, 0, 0, 0.05));
    border-radius: 3px;
    font-family: 'SauceCodePro Nerd Font', ui-monospace, monospace;
    font-size: 11.5px;
    color: var(--text-primary);
    white-space: pre-wrap;
    word-break: break-word;
  }
  .exec-help-body dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    column-gap: 12px;
    row-gap: 4px;
    margin: 0;
  }
  .exec-help-body dt {
    margin: 0;
  }
  .exec-help-body dd {
    margin: 0;
  }
  .exec-help-body ul {
    margin: 0;
    padding-left: 18px;
  }
  .exec-help-body li {
    margin: 2px 0;
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
  .textarea {
    width: 100%;
    padding: 6px 8px;
    border: 1px solid var(--color-border, var(--border));
    border-radius: var(--radius, 4px);
    background: var(--color-bg);
    color: inherit;
    font: inherit;
    font-size: 13px;
    resize: vertical;
  }
  .textarea.code {
    font-family: ui-monospace, monospace;
    font-size: 12px;
  }
  .radio-row {
    display: flex;
    gap: 16px;
  }
  .otp-cred {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 6px;
  }
  .link {
    background: none;
    border: 0;
    color: var(--color-primary, var(--accent));
    font: inherit;
    font-size: 12px;
    cursor: pointer;
    padding: 0;
    text-decoration: underline;
  }
  .readonly-summary {
    margin: 0;
    padding: 6px 10px;
    background: var(--bg-tertiary, rgba(0, 0, 0, 0.04));
    border-left: 3px solid var(--accent, var(--color-primary));
    border-radius: 4px;
    font-size: 12px;
    color: var(--text-secondary, var(--color-text));
  }
  /* Chonky's global input rule adds margin-bottom:1rem; flatten it
     inside the form so labels and inputs have consistent spacing. */
  .form :global(input),
  .form :global(select) {
    margin: 0 !important;
  }
  .form :global(.field-invalid) {
    border-color: var(--color-danger, #b91c1c) !important;
  }
  /* Widen the modal so the form's grid fields don't wrap awkwardly. */
  :global(.action-edit-modal) {
    max-width: 720px;
    width: calc(100% - 32px);
  }
</style>
