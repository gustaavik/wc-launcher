<script lang="ts">
  import type { Progress } from "../../bindings/github.com/gustaavik/wc-launcher/internal/install";
  import { mb, phaseLabel } from "./format";

  let { progress, oncancel }: { progress: Progress; oncancel: () => void } = $props();
</script>

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
  <button class="ghost" onclick={oncancel}>Cancel</button>
</div>

<style>
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
</style>
