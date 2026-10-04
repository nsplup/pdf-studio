import "./app.css";
import App from "./App.svelte";
import { initTaskEvents } from "./stores";
import { mount } from "svelte";

initTaskEvents();

const app = mount(App, { target: document.getElementById("app")! });
export default app;