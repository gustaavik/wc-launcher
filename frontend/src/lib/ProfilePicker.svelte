<script lang="ts">
  import { launcher } from "./state.svelte";

  const profiles = $derived(launcher.profiles);

  function pick(event: Event) {
    const select = event.currentTarget as HTMLSelectElement;
    void launcher.selectProfile(select.value);
  }

  // A pinned profile shows its version; Latest has none to show, because the
  // whole point of it is that the answer keeps changing.
  function label(name: string, tag: string, isLatest: boolean): string {
    return isLatest ? name : `${name} · ${tag}`;
  }
</script>

<div class="picker panel">
  <div class="head">
    <span class="key">Profile</span>
    <button class="ghost" onclick={() => launcher.go("profiles")}
      >Manage…</button
    >
  </div>

  <!-- A native select rather than a custom dropdown: the OS draws the popup
       outside the webview, so it is not clipped by this column, and keyboard
       and screen-reader behaviour come for free. -->
  <select
    aria-label="Profile"
    value={launcher.selectedProfile}
    onchange={pick}
    disabled={launcher.game.running ||
      launcher.installing ||
      profiles.length === 0}
  >
    {#each profiles as profile (profile.id)}
      <option value={profile.id}
        >{label(profile.name, profile.tag, profile.latest)}</option
      >
    {/each}
  </select>
</div>

<style>
  .picker {
    padding: var(--s-2);
    display: grid;
    gap: 0.45rem;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .key {
    font-size: 0.78rem;
    color: var(--muted);
  }
  .head :global(button) {
    font-size: 0.78rem;
  }
</style>
