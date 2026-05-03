<script>
  import QRCode from 'qrcode';

  let { uri, size = 240, alt = 'TOTP enrollment QR code' } = $props();
  let canvas;

  $effect(() => {
    if (!uri || !canvas) return;
    QRCode.toCanvas(canvas, uri, {
      width: size,
      margin: 0,
      color: { dark: '#000000ff', light: '#ffffffff' },
    }).catch(() => {
      // Render failure leaves the canvas blank; the surrounding panel
      // already shows the otpauth URI as a copyable fallback.
    });
  });
</script>

<div class="qr-wrap" style:width="{size}px" style:height="{size}px" role="img" aria-label={alt}>
  <canvas bind:this={canvas}></canvas>
</div>

<style>
  .qr-wrap {
    background: #ffffff;
    padding: 12px;
    border-radius: 8px;
    display: inline-block;
    line-height: 0;
  }
</style>
