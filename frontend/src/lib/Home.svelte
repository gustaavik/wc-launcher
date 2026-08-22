<script lang="ts">
  import { launcher } from "./state.svelte";

  let showLog = $state(false);
  let logEl = $state<HTMLPreElement | null>(null);

  const action = $derived(launcher.action);
  const latest = $derived(launcher.update?.latest ?? null);
  const progress = $derived(launcher.progress);

  // Follow the tail while the game runs, so a startup failure is the thing on
  // screen rather than something to scroll for.
  $effect(() => {
    void launcher.log.length;
    if (logEl && showLog) logEl.scrollTop = logEl.scrollHeight;
  });

  function press() {
    if (action.kind === "play") void launcher.play();
    else void launcher.install();
  }

  function phaseLabel(phase: string): string {
    switch (phase) {
      case "downloading": return "Downloading";
      case "verifying": return "Verifying";
      case "extracting": return "Unpacking";
      case "done": return "Done";
      case "cancelled": return "Cancelled";
      case "failed": return "Failed";
      default: return phase;
    }
  }

  function mb(bytes: number): string {
    return (bytes / 1024 / 1024).toFixed(1);
  }

  function shortDate(iso: string): string {
    if (!iso) return "";
    const at = new Date(iso);
    return Number.isNaN(at.getTime())
      ? ""
      : at.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  }
</script>

<div class="shell">
  <header>
    <div class="brand">
      <div class="mark" aria-hidden="true"></div>
      <strong>Wyvencraft</strong>
    </div>
    <div class="who">
      {#if launcher.account}
        <span class="name">{launcher.account.username}</span>
      {/if}
      <button class="ghost" onclick={() => (launcher.route = "settings")}>Settings</button>
      <button class="ghost" onclick={() => launcher.signOut()} disabled={launcher.game.running}>
        Sign out
      </button>
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

      <div class="versions panel">
        <div class="row">
          <span class="key">Installed</span>
          <span class="val">{launcher.update?.installedTag || "—"}</span>
        </div>
        <div class="row">
          <span class="key">Latest</span>
          <span class="val">{latest?.tag || "—"}</span>
        </div>
        {#if launcher.update?.message}
          <p class="dim small">{launcher.update.message}</p>
        {/if}
      </div>

      {#if progress && launcher.installing}
        <div class="progress panel">
          <div class="bar">
            <div
              class="fill"
              class:indeterminate={progress.percent < 0}
              style={progress.percent >= 0 ? `width:${progress.percent}%` : ""}
            ></div>
          </div>
          <div class="progress-text">
            <span>{phaseLabel(progress.phase)}</span>
            {#if progress.total > 0}
              <span>{mb(progress.received)} / {mb(progress.total)} MB</span>
            {/if}
          </div>
          <button class="ghost" onclick={() => launcher.cancelInstall()}>Cancel</button>
        </div>
      {/if}

      <button class="primary big" onclick={press} disabled={!action.enabled}>
        {action.label}
      </button>

      <div class="tools">
        <button class="ghost" onclick={() => launcher.check()} disabled={launcher.busy || launcher.installing}>
          Check for updates
        </button>
        {#if launcher.game.running}
          <button class="ghost" onclick={() => launcher.stopGame()}>Stop</button>
        {/if}
        <button class="ghost" onclick={() => (showLog = !showLog)}>
          {showLog ? "Hide log" : "Show log"}
        </button>
      </div>

      {#if showLog}
        <pre class="logbox panel" bind:this={logEl}>{launcher.log.join("\n") || "No output yet."}</pre>
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
  .brand { display: flex; align-items: center; gap: 0.6rem; }
  .mark {
    width: 22px;
    height: 22px;
    border-radius: 6px;
    background: var(--grad);
  }
  .who { display: flex; align-items: center; gap: 0.3rem; font-size: 0.85rem; }
  .name { color: var(--muted); margin-right: 0.4rem; }

  main {
    flex: 1;
    min-height: 0;
    display: grid;
    grid-template-columns: 1fr 280px;
    gap: var(--s-3);
    padding: var(--s-3);
  }

  .notes { display: flex; flex-direction: column; min-height: 0; }
  .notes-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--s-2);
    padding: var(--s-2) var(--s-3);
    border-bottom: 1px solid var(--border);
    flex: none;
  }
  .notes-head h2 { margin: 0; font-size: 1rem; }
  .date { color: var(--faint); font-size: 0.78rem; }
  .notes-body { overflow-y: auto; padding: var(--s-3); }

  .side {
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    min-height: 0;
  }

  .versions { padding: var(--s-2); display: flex; flex-direction: column; gap: 0.4rem; }
  .row { display: flex; justify-content: space-between; font-size: 0.85rem; }
  .key { color: var(--muted); }
  .val { font-variant-numeric: tabular-nums; }

  .progress { padding: var(--s-2); display: flex; flex-direction: column; gap: 0.5rem; }
  .bar {
    height: 6px;
    border-radius: 3px;
    background: rgba(0, 0, 0, 0.4);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    background: var(--grad);
    transition: width 150ms linear;
  }
  /* Verifying and unpacking have no measurable progress; a sliding bar says
     "working" without claiming a percentage it does not have. */
  .fill.indeterminate {
    width: 35%;
    animation: slide 1.1s ease-in-out infinite;
  }
  @keyframes slide {
    0% { margin-left: -35%; }
    100% { margin-left: 100%; }
  }
  @media (prefers-reduced-motion: reduce) {
    .fill.indeterminate { animation: none; width: 100%; opacity: 0.5; }
  }
  .progress-text {
    display: flex;
    justify-content: space-between;
    font-size: 0.78rem;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }

  .big { padding: 0.8rem; font-size: 1rem; }
  .tools { display: flex; flex-wrap: wrap; gap: 0.25rem; font-size: 0.8rem; }

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

  .dim { color: var(--faint); }
  .small { font-size: 0.78rem; margin: 0.2rem 0 0; }
</style>
