<script lang="ts">
  import { onMount } from "svelte";
  import Home from "./lib/Home.svelte";
  import Login from "./lib/Login.svelte";
  import Settings from "./lib/Settings.svelte";
  import { launcher } from "./lib/state.svelte";

  onMount(() => {
    launcher.listen();
    void launcher.start();
  });
</script>

{#if launcher.route === "loading"}
  <div class="splash">
    <div class="mark" aria-hidden="true"></div>
    <p>Starting…</p>
  </div>
{:else if launcher.route === "login"}
  <Login />
{:else if launcher.route === "settings"}
  <Settings />
{:else}
  <Home />
{/if}

<style>
  .splash {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--s-2);
    color: var(--faint);
    font-size: 0.85rem;
  }
  .mark {
    width: 44px;
    height: 44px;
    border-radius: 10px;
    background: var(--grad);
    animation: pulse 1.6s ease-in-out infinite;
  }
  @keyframes pulse {
    0%, 100% { opacity: 0.45; }
    50% { opacity: 1; }
  }
  @media (prefers-reduced-motion: reduce) {
    .mark { animation: none; }
  }
</style>
