import { renderAll } from "@coldtea/pr-lens-renderer";

const status = document.getElementById("status");
const tabs = document.getElementById("tabs");
const stage = document.getElementById("app");

function fail(message) {
  status.innerHTML = "";
  const p = document.createElement("p");
  p.textContent = message;
  status.appendChild(p);
}

const id = location.pathname
  .replace(/^.*\/c\//, "")
  .replace(/\.svg$/, "")
  .replace(/\/$/, "");

async function main() {
const res = await fetch(`/api/canvas/${id}`, { headers: { accept: "application/json" } });
if (!res.ok) {
  fail("No canvas answers to this address.");
  throw new Error(String(res.status));
}
const { document: doc, rev } = await res.json();

let assets;
try {
  ({ assets } = renderAll(doc, { themes: ["dark"] }));
} catch (e) {
  fail(`This document could not be drawn: ${e.message ?? e}`);
  throw e;
}

stage.innerHTML = "";
stage.id = "stage";

assets.forEach((asset, i) => {
  const button = document.createElement("button");
  button.textContent = (asset.asset.id ?? "")
    .replace(/-(light|dark|neutral)$/, "")
    .split("-")
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
  button.setAttribute("aria-selected", String(i === 0));
  button.addEventListener("click", () => {
    stage.querySelector("svg")?.remove();
    stage.insertAdjacentHTML("afterbegin", asset.svg);
    tabs.querySelectorAll("button").forEach((b) => b.setAttribute("aria-selected", "false"));
    button.setAttribute("aria-selected", "true");
  });
  tabs.appendChild(button);
});

stage.insertAdjacentHTML("afterbegin", assets[0].svg);
const revTag = document.createElement("span");
revTag.textContent = `rev ${rev}`;
revTag.style.cssText = "margin-left:auto;font-size:12px;color:#5c6470";
document.querySelector("header").appendChild(revTag);

}
main();
