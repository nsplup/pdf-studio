import "./app.css";
import App from "./App.svelte";
import { initTaskEvents } from "./stores";
import { AppService } from "./bindings/services";
import { mount } from "svelte";

initTaskEvents();

const app = mount(App, { target: document.getElementById("app")! });
export default app;

// 白屏优化：首帧渲染完成后才通知后端显示窗口
function signalReady() {
  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      AppService.Ready().catch(() => {
        // 兜底：绑定调用失败也要让窗口显示（由 Go 侧超时处理）
      });
    });
  });
}
if (document.readyState === "complete") {
  signalReady();
} else {
  window.addEventListener("load", signalReady, { once: true });
}
