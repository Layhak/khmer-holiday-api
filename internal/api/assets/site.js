(() => {
  "use strict";

  const copyText = async (text) => {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return;
    }

    const textArea = document.createElement("textarea");
    textArea.value = text;
    textArea.setAttribute("readonly", "");
    textArea.style.position = "fixed";
    textArea.style.opacity = "0";
    document.body.appendChild(textArea);
    textArea.select();
    const copied = document.execCommand("copy");
    textArea.remove();
    if (!copied) {
      throw new Error("copy unavailable");
    }
  };

  const showCopied = (button) => {
    const original = button.textContent;
    button.textContent = "Copied";
    button.disabled = true;
    window.setTimeout(() => {
      button.textContent = original;
      button.disabled = false;
    }, 1400);
  };

  document.addEventListener("click", async (event) => {
    const button = event.target.closest("[data-copy-target]");
    if (!button || button.disabled) {
      return;
    }

    const target = document.getElementById(button.dataset.copyTarget);
    if (!target) {
      return;
    }

    try {
      await copyText(target.textContent.trim());
      showCopied(button);
    } catch {
      button.textContent = "Copy failed";
      window.setTimeout(() => {
        button.textContent = button.classList.contains("response-copy")
          ? "Copy response"
          : "Copy request";
      }, 1800);
    }
  });

  const loadResponse = async (details) => {
    if (details.dataset.loaded === "true" || details.dataset.loading === "true") {
      return;
    }

    const example = details.closest("[data-endpoint]");
    const output = details.querySelector("pre code");
    const status = details.querySelector(".response-status");
    const copyButton = details.querySelector(".response-copy");
    if (!example || !output || !status || !copyButton) {
      return;
    }

    details.dataset.loading = "true";
    output.textContent = "Loading current response…";
    status.textContent = "";
    status.classList.remove("error");

    try {
      const response = await fetch(example.dataset.endpoint, {
        headers: { Accept: "application/json" },
      });
      const text = await response.text();
      let formatted = text;
      try {
        formatted = JSON.stringify(JSON.parse(text), null, 2);
      } catch {
        // Preserve a non-JSON upstream error as readable text.
      }

      output.textContent = formatted.trim();
      if (!response.ok) {
        throw new Error(`Request returned HTTP ${response.status}`);
      }

      details.dataset.loaded = "true";
      copyButton.disabled = false;
      status.textContent = "Live response loaded.";
    } catch (error) {
      status.textContent = error instanceof Error
        ? error.message
        : "The live response could not be loaded.";
      status.classList.add("error");
    } finally {
      delete details.dataset.loading;
    }
  };

  document.querySelectorAll(".live-response").forEach((details) => {
    details.addEventListener("toggle", () => {
      if (details.open) {
        loadResponse(details);
      }
    });
    if (details.open) {
      loadResponse(details);
    }
  });
})();
