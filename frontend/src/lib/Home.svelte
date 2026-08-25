<script lang="ts">
  import LauncherUpdate from "./LauncherUpdate.svelte";
  import ProfilePicker from "./ProfilePicker.svelte";
  import ProgressPanel from "./ProgressPanel.svelte";
  import { shortDate } from "./format";
  import { launcher } from "./state.svelte";

  let showLog = $state(false);
  let logEl = $state<HTMLPreElement | null>(null);

  const action = $derived(launcher.action);
  // The profile's own release when it has one, so a pinned profile shows the
  // changelog for the build it actually runs. Falls back to the newest.
  const latest = $derived(
    launcher.update?.target ?? launcher.update?.latest ?? null,
  );
  const progress = $derived(launcher.progress);

  // Follow the tail while the game runs, so a startup failure is the thing on
  // screen rather than something to scroll for.
  $effect(() => {
    void launcher.log.length;
    if (logEl && showLog) logEl.scrollTop = logEl.scrollHeight;
  });

  function press() {
    // Installing needs an account, so the button offers the sign-in instead of
    // a download that would be refused.
    if (action.kind === "signin") launcher.go("login");
    else if (action.kind === "play") void launcher.play();
    // A required update is one action, not two: the Latest profile does not
    // offer the old build to fall back on, so there is nothing to stop for.
    else if (action.kind === "update") void launcher.updateAndPlay();
    else void launcher.install();
  }
</script>

<div class="shell">
  <header>
    <div class="brand">
      <img class="mark" src="/logo.png" alt="" draggable="false" />
      <strong>Wyvencraft</strong>
    </div>
    <div class="who">
      {#if launcher.account}
        <span class="name">{launcher.account.username}</span>
      {:else}
        <span class="name offline">Playing offline</span>
      {/if}
      <button class="ghost" onclick={() => launcher.go("settings")}
        >Settings</button
      >
      {#if launcher.account}
        <button
          class="ghost"
          onclick={() => launcher.signOut()}
          disabled={launcher.game.running}
        >
          Sign out
        </button>
      {:else}
        <button
          class="ghost"
          onclick={() => launcher.go("login")}
          disabled={launcher.game.running}
        >
          Sign in
        </button>
      {/if}
    </div>
  </header>

  <main>
    <section class="notes panel">
      <div class="notes-head">
        <h2>{latest ? latest.name || latest.tag : "Changelog"}</h2>
        {#if latest?.publishedAt}
          <span class="date">{shortDate(latest.publishedAt)}</span>
        {/if}
      </div>
      <div class="notes-body">
        {#if latest?.notesHtml}
          <!-- Rendered and sanitised on the Go side: goldmark drops raw HTML,
               so nothing a release author writes can become markup here. -->
          <div class="changelog">{@html latest.notesHtml}</div>
        {:else if launcher.busy}
          <p class="dim">Checking for updates…</p>
        {:else}
          <p class="dim">No release notes to show.</p>
        {/if}
      </div>
    </section>

    <aside class="side">
      {#if launcher.banner}
        <p class="error">{launcher.banner}</p>
      {/if}

      <LauncherUpdate />

      <ProfilePicker />

      <div class="versions panel">
        <div class="row">
          <span class="key">Installed</span>
          <span class="val">{launcher.update?.installedTag || "—"}</span>
        </div>
        <div class="row">
          <span class="key"
            >{launcher.profile && !launcher.profile.latest
              ? "Pinned to"
              : "Latest"}</span
          >
          <span class="val"
            >{launcher.update?.target?.tag ??
              launcher.update?.latest?.tag ??
              "—"}</span
          >
        </div>
        {#if launcher.update?.message}
          <p class="dim small">{launcher.update.message}</p>
        {/if}
      </div>

      {#if progress && launcher.installing}
        <ProgressPanel {progress} oncancel={() => launcher.cancelInstall()} />
      {/if}

      <button class="primary big" onclick={press} disabled={!action.enabled}>
        {action.label}
      </button>
      {#if launcher.blockedReason}
        <p class="dim small blocked">{launcher.blockedReason}</p>
      {/if}

      <div class="tools">
        <button
          class="ghost"
          onclick={() => {
            launcher.selfDismissed = false;
            void launcher.check();
            void launcher.checkSelf();
          }}
          disabled={launcher.busy || launcher.installing}
        >
          Check for updates
        </button>
        {#if launcher.game.running}
          <button class="ghost" onclick={() => launcher.stopGame()}>Stop</button
          >
        {/if}
        <button class="ghost" onclick={() => (showLog = !showLog)}>
          {showLog ? "Hide log" : "Show log"}
        </button>
      </div>

      {#if showLog}
        <pre class="logbox panel" bind:this={logEl}>{launcher.log.join("\n") ||
            "No output yet."}</pre>
      {/if}
    </aside>
  </main>
</div>

<style>
  .shell {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    /* Left padding clears the macOS traffic lights, which the frameless
       window draws over the content. */
    padding: var(--s-2) var(--s-3) var(--s-2) 5.5rem;
    border-bottom: 1px solid var(--border);
    flex: none;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 0.6rem;
  }
  .mark {
    width: 22px;
    height: 22px;
    display: block;
    object-fit: contain;
    /* The header is a window-drag region; without this the webview starts an
       image drag instead of moving the window. */
    -webkit-user-drag: none;
  }
  .who {
    display: flex;
    align-items: center;
    gap: 0.3rem;
    font-size: 0.85rem;
  }
  .name {
    color: var(--muted);
    margin-right: 0.4rem;
  }
  .name.offline {
    color: var(--faint);
  }

  main {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: 1fr 280px;
    gap: var(--s-3);
    padding: var(--s-3);
  }

  .notes {
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
  .notes-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--s-2);
    padding: var(--s-2) var(--s-3);
    border-bottom: 1px solid var(--border);
    flex: none;
  }
  .notes-head h2 {
    margin: 0;
    font-size: 1rem;
  }
  .date {
    color: var(--faint);
    font-size: 0.78rem;
  }
  .notes-body {
    overflow-y: auto;
    padding: var(--s-3);
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    min-height: 0;
  }

  .versions {
    padding: var(--s-2);
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .row {
    display: flex;
    justify-content: space-between;
    font-size: 0.85rem;
  }
  .key {
    color: var(--muted);
  }
  .val {
    font-variant-numeric: tabular-nums;
  }

  .big {
    padding: 0.8rem;
    font-size: 1rem;
  }
  .tools {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    font-size: 0.8rem;
  }

  .logbox {
    flex: 1;
    min-height: 120px;
    margin: 0;
    padding: var(--s-2);
    overflow: auto;
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.72rem;
    line-height: 1.5;
    color: var(--muted);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }

  .dim {
    color: var(--faint);
  }
  .small {
    font-size: 0.78rem;
    margin: 0.2rem 0 0;
  }
  /* Sits directly under the big button and explains it, so it reads as part of
     the same control rather than as another status line. */
  .blocked {
    text-align: center;
    line-height: 1.5;
  }
</style>
