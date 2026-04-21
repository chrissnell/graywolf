<script module>
  // Re-export Chonky's Modal sub-components for callers that need direct
  // access (e.g. custom header/body/footer layouts). The wrapper below
  // is the ergonomic default; these named exports are the escape hatch.
  import { Modal as ChonkyModal } from '@chrissnell/chonky-ui';
  export const Header = ChonkyModal.Header;
  export const Body = ChonkyModal.Body;
  export const Footer = ChonkyModal.Footer;
  export const Close = ChonkyModal.Close;
  export const Description = ChonkyModal.Description;
</script>

<script>
  let {
    open = $bindable(false),
    title = '',
    onClose = undefined,
    class: className = '',
    children,
  } = $props();

  function handleClose() {
    open = false;
    onClose?.();
  }
</script>

<ChonkyModal bind:open onClose={handleClose} class={className}>
  <Header>
    {#if title}
      <h3 class="modal-title">{title}</h3>
    {/if}
    <Close />
  </Header>
  <Body>
    {@render children()}
  </Body>
</ChonkyModal>

<style>
  .modal-title {
    font-size: 16px;
    font-weight: 600;
  }
</style>
