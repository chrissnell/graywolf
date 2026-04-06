<script>
  import { Modal as ChonkyModal } from '@chrissnell/chonky-ui';

  let { open = $bindable(false), title = '', children } = $props();

  const Header = ChonkyModal.Header;
  const Body = ChonkyModal.Body;
  const Close = ChonkyModal.Close;
</script>

<ChonkyModal bind:open onClose={() => open = false}>
  <Header>
    <h3 class="modal-title">{title}</h3>
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
