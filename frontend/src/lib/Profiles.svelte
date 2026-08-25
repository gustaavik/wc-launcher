<script lang="ts">
  import { onMount } from "svelte";
  import { shortDate } from "./format";
  import { launcher } from "./state.svelte";

  // Which profile's row is expanded for editing. Only one at a time: two open
  // name fields is two ways to lose an edit.
  let editing = $state<string | null>(null);
  let editName = $state("");
  let editTag = $state("");

  let newName = $state("");
  let newTag = $state("");

  const releases = $derived(launcher.releases);
  const locked = $derived(launcher.game.running || launcher.profileBusy);

  onMount(() => {
    void launcher.loadReleases();
    void launcher.loadBuilds();
  });

  function beginEdit(id: string, name: string, tag: string) {
    editing = id;
    editName = name;
    editTag = tag;
  }

  async function saveEdit(id: string) {
    // Name and version are separate mutations on the Go side, so a rejected
    // rename must not silently take the version change with it.
    if (!(await launcher.renameProfile(id, editName))) return;
    if (editTag && !(await launcher.retagProfile(id, editTag))) return;
    editing = null;
  }

  async function create() {
    if (await launcher.createProfile(newName, newTag)) {
      newName = "";
      newTag = "";
    }
  }

  /** How a release reads in the dropdown. */
  function option(
    tag: string,
    prerelease: boolean,
    installed: boolean,
  ): string {
    const notes = [];
    if (prerelease) notes.push("pre-release");
    if (installed) notes.push("installed");
    return notes.length ? `${tag} · ${notes.join(", ")}` : tag;
  }
</script>

<div class="shell">
  <header>
    <button class="ghost" onclick={() => (launcher.route = "home")}
      >← Back</button
    >
    <strong>Profiles</strong>
    <span></span>
  </header>

  <div class="body">
    {#if launcher.profileError}
      <p class="error">{launcher.profileError}</p>
    {/if}
    {#if launcher.game.running}
      <p class="notice">
        Wyvencraft is running. Profiles can be switched, but not changed, until
        it quits.
      </p>
    {/if}

    <section class="panel">
      <h2>Your profiles</h2>
      <p class="hint">
        A profile decides which build of Wyvencraft starts. Saves are shared —
        every profile plays the same worlds.
      </p>

      <ul class="rows">
        {#each launcher.profiles as profile (profile.id)}
          <li class:selected={profile.id === launcher.selectedProfile}>
            <div class="row">
              <div class="who">
                <span class="name">{profile.name}</span>
                <span class="meta">
                  {profile.latest ? "always the newest release" : profile.tag}
                  {#if !profile.installed}<span class="warn">
                      · not installed</span
                    >{/if}
                </span>
              </div>
              <div class="acts">
                {#if profile.id !== launcher.selectedProfile}
                  <button
                    class="ghost"
                    onclick={() => launcher.selectProfile(profile.id)}
                    >Select</button
                  >
                {:else}
                  <span class="badge">Selected</span>
                {/if}
                {#if !profile.latest}
                  <button
                    class="ghost"
                    disabled={locked}
                    onclick={() =>
                      editing === profile.id
                        ? (editing = null)
                        : beginEdit(profile.id, profile.name, profile.tag)}
                  >
                    {editing === profile.id ? "Cancel" : "Edit"}
                  </button>
                  <button
                    class="ghost danger"
                    disabled={locked}
                    onclick={() => launcher.deleteProfile(profile.id)}
                  >
                    Delete
                  </button>
                {/if}
              </div>
            </div>

            {#if profile.latest}
              <p class="hint locked-note">
                The built-in profile. It cannot be renamed or removed, and it
                always updates to the newest release before it will play.
              </p>
            {/if}

            {#if editing === profile.id}
              <div class="edit">
                <label>
                  <span class="key">Name</span>
                  <input bind:value={editName} maxlength="40" />
                </label>
                <label>
                  <span class="key">Version</span>
                  <select
                    bind:value={editTag}
                    disabled={!releases || releases.length === 0}
                  >
                    <!-- The pinned tag may have aged out of the list; keep it
                         selectable so opening the editor cannot silently repin. -->
                    {#if releases && !releases.some((r) => r.tag === editTag)}
                      <option value={editTag}
                        >{editTag} · no longer published</option
                      >
                    {/if}
                    {#each releases ?? [] as release (release.tag)}
                      <option value={release.tag} disabled={!release.supported}>
                        {option(
                          release.tag,
                          release.prerelease,
                          release.installed,
                        )}
                      </option>
                    {/each}
                  </select>
                </label>
                <button
                  class="primary"
                  disabled={locked}
                  onclick={() => saveEdit(profile.id)}>Save</button
                >
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    </section>

    <section class="panel">
      <h2>New profile</h2>
      {#if releases === null}
        <p class="hint">Loading the available versions…</p>
      {:else if releases.length === 0}
        <p class="hint">
          No versions to pin to. Sign in and check that this server offers game
          downloads.
        </p>
      {/if}

      <div class="create">
        <label>
          <span class="key">Name</span>
          <input bind:value={newName} maxlength="40" placeholder="Speedrun" />
        </label>
        <label>
          <span class="key">Version</span>
          <select
            bind:value={newTag}
            disabled={!releases || releases.length === 0}
          >
            <option value="" disabled>Pick a version…</option>
            {#each releases ?? [] as release (release.tag)}
              <option value={release.tag} disabled={!release.supported}>
                {option(release.tag, release.prerelease, release.installed)}
              </option>
            {/each}
          </select>
        </label>
        <button
          class="primary"
          disabled={locked || !newName || !newTag}
          onclick={create}>Create</button
        >
      </div>
      <p class="hint">
        A greyed-out version publishes no build for this computer. Pinning to
        one would offer a download that cannot succeed.
      </p>
    </section>

    <section class="panel">
      <h2>Installed builds</h2>
      {#if launcher.builds.length === 0}
        <p class="hint">Nothing downloaded yet.</p>
      {:else}
        <ul class="builds">
          {#each launcher.builds as build (build.tag)}
            <li>
              <code>{build.tag}</code>
              <span class="meta"
                >{build.installedAt ? shortDate(build.installedAt) : ""}</span
              >
            </li>
          {/each}
        </ul>
        <p class="hint">
          Old builds are cleared out as new ones arrive, but a build some
          profile is pinned to is always kept.
        </p>
      {/if}
    </section>
  </div>
</div>

<style>
  .shell {
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  header {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    /* Clears the macOS traffic lights, as on the other screens. */
    padding: var(--s-2) var(--s-3) var(--s-2) 5.5rem;
    border-bottom: 1px solid var(--border);
    flex: none;
    font-size: 0.9rem;
  }
  header button {
    justify-self: start;
  }

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
  h2 {
    margin: var(--s-1) 0 0;
    font-size: 0.9rem;
  }
  .hint {
    margin: 0;
    font-size: 0.78rem;
    color: var(--faint);
    line-height: 1.5;
  }

  .rows,
  .builds {
    list-style: none;
    margin: 0.25rem 0 0;
    padding: 0;
    display: grid;
    gap: 0.4rem;
  }
  .rows li {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 0.6rem 0.75rem;
    background: var(--panel-2);
  }
  .rows li.selected {
    border-color: var(--accent-2);
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--s-1);
  }
  .who {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }
  .name {
    font-size: 0.85rem;
  }
  .meta {
    font-size: 0.75rem;
    color: var(--faint);
  }
  .warn {
    color: var(--accent);
  }
  .acts {
    display: flex;
    align-items: center;
    gap: 0.2rem;
    flex: none;
  }
  .acts :global(button) {
    font-size: 0.78rem;
  }
  .badge {
    font-size: 0.72rem;
    color: var(--accent);
    padding: 0.2rem 0.45rem;
  }
  :global(button.ghost.danger:hover:not(:disabled)) {
    color: var(--bad);
  }
  .locked-note {
    margin-top: 0.4rem;
  }

  .edit,
  .create {
    display: grid;
    grid-template-columns: 1fr 1fr auto;
    gap: 0.5rem;
    align-items: end;
    margin-top: 0.6rem;
  }
  .create {
    margin-top: 0.25rem;
  }
  label {
    display: grid;
    gap: 0.2rem;
    min-width: 0;
  }
  .key {
    font-size: 0.78rem;
    color: var(--muted);
  }

  .builds li {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--s-1);
  }
  code {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 0.75rem;
    background: rgba(0, 0, 0, 0.3);
    border-radius: 4px;
    padding: 0.1rem 0.35rem;
  }

  /* Below this the three-column rows stop fitting and the version dropdown
     becomes unusable. */
  @media (max-width: 560px) {
    .edit,
    .create {
      grid-template-columns: 1fr;
      align-items: stretch;
    }
  }
</style>
