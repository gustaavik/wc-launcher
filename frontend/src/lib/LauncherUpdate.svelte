<script lang="ts">
  // The launcher's own update, which is not the game's: it comes from GitHub,
  // it works signed out, and applying it restarts this window.
  import ProgressPanel from "./ProgressPanel.svelte";
  import { launcher } from "./state.svelte";

  const status = $derived(launcher.selfUpdate);
  const tag = $derived(status?.latest?.tag ?? "");
</script>

{#if launcher.selfUpdateVisible && status}
  <div class="strip panel" class:ready={status.staged}>
    <div class="head">
      <span class="title">
        {#if status.staged}
          Launcher {tag} is ready
        {:else}
          Launcher {tag} is available
        {/if}
      </span>
      <button class="close" onclick={() => (launcher.selfDismissed = true)} aria-label="Dismiss">×</button>
    </div>

    <p class="sub">You have {status.current}.</p>

    {#if !status.writable}
      <!-- Downloading would only fail at the last step, so the button is not
           offered at all. -->
      <p class="sub warn">
        The launcher cannot replace itself where it is installed. Move it to your
        Applications folder and check again.
      </p>
    {:else if launcher.selfBusy && launcher.selfProgress}
      <ProgressPanel progress={launcher.selfProgress} oncancel={() => launcher.cancelSelfInstall()} />
    {:else if status.staged}
      <button class="primary small" onclick={() => launcher.restartToUpdate()} disabled={launcher.game.running}>
        Restart to update
      </button>
      {#if launcher.game.running}
        <p class="sub">The launcher restarts once Wyvencraft has closed.</p>
      {/if}
    {:else}
      <button class="primary small" onclick={() => launcher.installSelf()} disabled={launcher.game.running}>
        Download
      </button>
    {/if}
  </div>
{/if}

<style>
  .strip {
    padding: var(--s-2);
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    border-left: 2px solid #6ea8fe;
  }
  .strip.ready { border-left-color: #58c07a; }
  .head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; }
  .title { font-size: 0.85rem; font-weight: 600; }
  .sub { margin: 0; font-size: 0.78rem; color: var(--faint); line-height: 1.45; }
  .warn { color: var(--muted); }
  .close {
    background: none;
    border: none;
    color: var(--faint);
    font-size: 1rem;
    line-height: 1;
    padding: 0 0.2rem;
    cursor: pointer;
  }
  .close:hover { color: var(--muted); }
  .small { padding: 0.4rem; font-size: 0.85rem; }
</style>
