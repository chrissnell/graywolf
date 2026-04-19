<script>
  // "New message" modal launched from the sidebar/list "New" button
  // or the `?compose=1` deep link. Collects a To: callsign (via
  // CallsignAutocomplete) and delegates the actual send to the
  // parent — on success it closes and the parent navigates to the
  // newly-opened thread.
  //
  // Tactical compose skips this modal: `?compose=tactical:NET`
  // navigates straight into the tactical thread's compose bar with
  // the locked-pill To.

  import { Button, Icon, Modal } from '@chrissnell/chonky-ui';
  import CallsignAutocomplete from './CallsignAutocomplete.svelte';
  import ComposeBar from './ComposeBar.svelte';

  /** @type {{
   *    open: boolean,
   *    onSend?: (text: string, to: string) => Promise<any>,
   *    onClose?: () => void,
   *  }}
   */
  let {
    open = $bindable(false),
    onSend,
    onClose,
  } = $props();

  let to = $state('');

  function close() {
    open = false;
    onClose?.();
  }

  async function send(text, targetOverride) {
    const target = (targetOverride || to || '').trim().toUpperCase();
    if (!target) return;
    await onSend?.(text, target);
    close();
  }
</script>

<Modal bind:open onClose={close}>
  <Modal.Header>
    <h3 class="title">New message</h3>
    <Modal.Close aria-label="Close">
      <Icon name="x" size="sm" />
    </Modal.Close>
  </Modal.Header>
  <Modal.Body>
    <div class="to-field">
      <label for="compose-new-to">To</label>
      <CallsignAutocomplete
        bind:value={to}
        placeholder="Callsign or APRS service"
        onCommit={(v) => to = v}
        autofocus={true}
      />
    </div>
    <!-- ComposeBar in "thread" mode so it doesn't render its own
         To: field (the modal wrapper owns that above). `embedded`
         flips the compose bar from position:absolute → relative so
         the modal body handles layout. -->
    <ComposeBar
      mode="thread"
      dmPeer={to}
      threadHasMessages={false}
      autoFocus={false}
      embedded={true}
      onSend={(text) => send(text)}
    />
  </Modal.Body>
</Modal>

<style>
  .title {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    font-family: var(--font-mono);
  }
  .to-field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 16px;
  }
  .to-field label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 1px;
    text-transform: uppercase;
    color: var(--color-text-dim);
  }
  :global(.modal-body .compose) {
    position: relative;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    padding: 8px;
  }
</style>
