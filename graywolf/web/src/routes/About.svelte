<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';

  let version = $state('');

  onMount(async () => {
    try {
      const d = await fetch('/api/version').then(r => r.json());
      version = d.version || '';
    } catch {}
  });
</script>

<div class="about-content">
  <p class="about-version">Graywolf v.{version}</p>
  <p class="about-copyright">&copy; 2026 Chris Snell, NW5W</p>
  <p class="about-license">
    Released under the
    <a href="https://www.gnu.org/licenses/old-licenses/gpl-2.0.html" target="_blank" rel="noopener">
      GNU General Public License v2.0</a>.
    You are free to redistribute and modify this software under the terms of the GPL 2.0.
  </p>
  <blockquote class="about-quote">
    <p>
      "And behold, I tell you these things that ye may learn wisdom; that ye may learn
      that when ye are in the service of your fellow beings ye are only in the service of your God."
    </p>
    <cite>
      <a href="https://www.churchofjesuschrist.org/study/scriptures/bofm/mosiah/2?lang=eng&id=p17#p17"
         target="_blank" rel="noopener">
        Mosiah 2:17
      </a>
    </cite>
  </blockquote>
</div>

<style>
  .about-content {
    max-width: 560px;
  }

  .about-version {
    font-size: 14px;
    font-weight: 700;
    margin: 0 0 8px;
  }

  .about-copyright {
    font-size: 14px;
    color: var(--text-secondary);
    margin: 0 0 16px;
  }

  .about-license {
    font-size: 14px;
    line-height: 1.6;
    color: var(--text-secondary);
    margin: 0 0 32px;
  }

  .about-license a {
    color: var(--accent);
  }

  .about-quote {
    margin: 0;
    padding: 16px 20px;
    border-left: 3px solid var(--accent);
    background: var(--bg-secondary);
    border-radius: 0 var(--radius) var(--radius) 0;
  }

  .about-quote p {
    font-style: italic;
    line-height: 1.7;
    margin: 0 0 12px;
    color: var(--text-primary);
  }

  .about-quote cite {
    font-style: normal;
    font-size: 13px;
    color: var(--text-secondary);
    display: block;
    text-align: right;
  }

  .about-quote cite a {
    color: var(--accent);
  }
</style>
