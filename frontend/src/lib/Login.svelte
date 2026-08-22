<script lang="ts">
  import { onMount } from "svelte";
  import { AuthService, UpdateService } from "../../bindings/github.com/gustaavik/wc-launcher/internal/services";
  import type { ServerInfo } from "../../bindings/github.com/gustaavik/wc-launcher/internal/services";
  import { launcher } from "./state.svelte";

  let username = $state("");
  let password = $state("");
  let error = $state("");
  let busy = $state(false);
  let server = $state<ServerInfo | null>(null);

  onMount(async () => {
    // Prefilled after an expired session, so the player types one field.
    username = await AuthService.StoredUsername();
    // Probed unauthenticated, so a wrong server address is visible before
    // anyone blames their password for it.
    server = await UpdateService.Server();
  });

  async function submit(event: Event) {
    event.preventDefault();
    if (busy) return;

    busy = true;
    error = "";
    try {
      const result = await AuthService.Login(username, password);
      if (result.error) {
        error = result.error;
        return;
      }
      // Cleared before anything else can await: it should not sit in memory
      // any longer than the request needs it.
      password = "";
      launcher.account = result.account ?? null;
      launcher.route = "home";
      void launcher.check();
    } finally {
      busy = false;
    }
  }
</script>

<div class="wrap">
  <form class="card panel" onsubmit={submit}>
    <div class="brand">
      <div class="mark" aria-hidden="true"></div>
      <div>
        <h1>Wyvencraft</h1>
        <p class="sub">Sign in to play</p>
      </div>
    </div>

    {#if launcher.banner}
      <p class="notice">{launcher.banner}</p>
    {/if}

    <label>
      <span>Username</span>
      <input
        bind:value={username}
        autocomplete="username"
        autocapitalize="off"
        spellcheck="false"
        placeholder="gustav"
      />
    </label>

    <label>
      <span>Password</span>
      <input type="password" bind:value={password} autocomplete="current-password" />
    </label>

    {#if error}
      <p class="error">{error}</p>
    {/if}

    <button class="primary" type="submit" disabled={busy}>
      {busy ? "Signing in…" : "Sign in"}
    </button>

    <footer>
      {#if server && !server.reachable}
        <span class="warn">{server.message}</span>
      {:else if server}
        <span>Connected to the account server</span>
      {:else}
        <span>Checking the account server…</span>
      {/if}
      <button class="ghost" type="button" onclick={() => (launcher.route = "settings")}>
        Settings
      </button>
    </footer>
  </form>
</div>

<style>
  .wrap {
    height: 100%;
    display: grid;
    place-items: center;
    padding: var(--s-3);
  }
  .card {
    width: min(400px, 100%);
    padding: var(--s-4);
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    margin-bottom: var(--s-1);
  }
  .mark {
    width: 40px;
    height: 40px;
    border-radius: 9px;
    background: var(--grad);
    flex: none;
  }
  h1 {
    margin: 0;
    font-size: 1.35rem;
    letter-spacing: 0.01em;
  }
  .sub {
    margin: 0.15rem 0 0;
    color: var(--muted);
    font-size: 0.85rem;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
    font-size: 0.82rem;
    color: var(--muted);
  }
  footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--s-2);
    margin-top: var(--s-1);
    font-size: 0.78rem;
    color: var(--faint);
  }
  .warn { color: var(--bad); }
</style>
