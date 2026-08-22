<script lang="ts">
  import { onMount } from "svelte";
  import { AuthService } from "../../bindings/github.com/gustaavik/wc-launcher/internal/services";
  import type { SettingsView } from "../../bindings/github.com/gustaavik/wc-launcher/internal/services";
  import LauncherUpdate from "./LauncherUpdate.svelte";
  import { launcher } from "./state.svelte";

  let settings = $state<SettingsView | null>(null);
  let authUrl = $state("");
  let logFilter = $state("");
  let message = $state("");
  let busy = $state(false);
  let checkingLauncher = $state(false);

  onMount(async () => {
    settings = await AuthService.Settings();
    authUrl = settings.authUrl;
    logFilter = settings.logFilter;
  });

  async function save() {
    busy = true;
    message = "";
    try {
      const result = await AuthService.SaveSettings(authUrl, logFilter);
      if (result.error) {
        message = result.error;
        return;
      }
      launcher.account = result.account ?? null;
      // Changing the account server invalidates the session — tokens are
      // issued by one server and meaningless to another — so the Go side
      // signs out. Follow it rather than showing a stale signed-in screen.
      message = launcher.account ? "Saved." : "Saved. Sign in again to continue.";
    } finally {
      busy = false;
    }
  }

  function back() {
    launcher.route = launcher.account ? "home" : "login";
  }

  async function checkLauncher() {
    checkingLauncher = true;
    // Undo an earlier dismissal: asking is a request to be told again.
    launcher.selfDismissed = false;
    try {
      await launcher.checkSelf();
    } finally {
      checkingLauncher = false;
    }
  }
</script>

<div class="shell">
  <header>
    <button class="ghost" onclick={back}>← Back</button>
    <strong>Settings</strong>
    <span></span>
  </header>

  <div class="body">
    <section class="panel">
      <h2>Account server</h2>
      <p class="hint">
        Leave empty to use the default.
        {#if settings}<code>{settings.defaultAuthUrl}</code>{/if}
      </p>
      <input bind:value={authUrl} placeholder={settings?.defaultAuthUrl ?? ""} spellcheck="false" />
      <p class="hint">Changing this signs you out: a session belongs to one server.</p>
    </section>

    <section class="panel">
      <h2>Game logging</h2>
      <p class="hint">
        Passed to the game as <code>RUST_LOG</code>. Empty means its own default
        (<code>info,wyvencraft=info</code>). Try <code>debug</code> when diagnosing a crash.
      </p>
      <input bind:value={logFilter} placeholder="info,wyvencraft=info" spellcheck="false" />
    </section>

    <section class="panel">
      <h2>On this computer</h2>
      <div class="paths">
        <div>
          <span class="key">Saves and settings</span>
          <code>{settings?.dataDir ?? "…"}</code>
        </div>
        <div>
          <span class="key">Installed builds</span>
          <code>{settings?.versionsDir ?? "…"}</code>
        </div>
      </div>
      <p class="hint">Saves live outside the install, so an update never touches them.</p>
    </section>

    <section class="panel">
      <h2>Launcher</h2>
      <div class="paths">
        <div>
          <span class="key">Version</span>
          <code>{launcher.selfUpdate?.current ?? "…"}</code>
        </div>
      </div>
      <p class="hint">
        The launcher updates itself from GitHub, separately from the game. It
        does not need an account server, or an account.
      </p>
      {#if launcher.selfUpdate?.message}
        <p class="hint">{launcher.selfUpdate.message}</p>
      {:else if launcher.selfUpdate && !launcher.selfUpdate.updateAvailable}
        <p class="hint">This is the newest launcher.</p>
      {/if}
      <LauncherUpdate />
      <button class="ghost" onclick={checkLauncher} disabled={checkingLauncher || launcher.selfBusy}>
        {checkingLauncher ? "Checking…" : "Check for a launcher update"}
      </button>
    </section>

    {#if message}
      <p class="notice">{message}</p>
    {/if}

    <button class="primary" onclick={save} disabled={busy || launcher.game.running}>
      {busy ? "Saving…" : "Save"}
    </button>
    {#if launcher.game.running}
      <p class="hint">Settings cannot change while Wyvencraft is running.</p>
    {/if}
  </div>
</div>

<style>
  .shell { height: 100%; display: flex; flex-direction: column; }
  header {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    padding: var(--s-2) var(--s-3) var(--s-2) 5.5rem;
    border-bottom: 1px solid var(--border);
    flex: none;
    font-size: 0.9rem;
  }
  header button { justify-self: start; }

  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--s-3);
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    max-width: 620px;
    width: 100%;
    margin: 0 auto;
  }
  section {
    padding: var(--s-2) var(--s-3) var(--s-3);
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  h2 { margin: var(--s-1) 0 0; font-size: 0.9rem; }
  .hint { margin: 0; font-size: 0.78rem; color: var(--faint); line-height: 1.5; }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.75rem;
    background: rgba(0, 0, 0, 0.3);
    border-radius: 4px;
    padding: 0.1rem 0.35rem;
    overflow-wrap: anywhere;
    -webkit-user-select: text;
    user-select: text;
  }
  .paths { display: flex; flex-direction: column; gap: 0.5rem; }
  .paths div { display: flex; flex-direction: column; gap: 0.2rem; }
  .key { font-size: 0.78rem; color: var(--muted); }
</style>
