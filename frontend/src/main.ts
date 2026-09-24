import { mount } from 'svelte'
import App from './App.svelte'
import { detectOS } from './lib/platform'

// Set before the first paint, so the header inset never jumps.
document.documentElement.dataset.os = detectOS(navigator.userAgent)

mount(App, { target: document.getElementById('app')! })
