import "./app.css";
import App from "./App.svelte";
import { initTaskEvents } from "./stores";
import { AppService } from "@bindings";
import { mount } from "svelte";

initTaskEvents();

const app = mount(App, { target: document.getElementById("app")! });
export default app;

// 白屏优化：首帧渲染完成后才通知后端显示窗口
function signalReady() {
  requestAnimationFrame(() => {
    AppService.Ready()
  });
}
if (document.readyState === "complete") {
  signalReady();
} else {
  window.addEventListener("load", signalReady, { once: true });
}
