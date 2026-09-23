(() => {
    "use strict";

    document.documentElement.classList.add("js");

    const nameCharacters = /^[\p{Script=Cyrillic}-]*$/u;

    document.querySelectorAll("[data-suggestion-group]").forEach((group, groupIndex) => {
        const inputs = Array.from(group.querySelectorAll("[data-suggestion-row] input"));
        const counter = group.querySelector("[data-suggestion-count]");
        const characterHint = group.querySelector("[data-character-hint]");

        if (!inputs.length || !counter || !characterHint) {
            return;
        }

        characterHint.id = `character-hint-${groupIndex + 1}`;

        const updateCounter = () => {
            const filled = inputs.filter((input) => input.value.trim() !== "").length;
            counter.textContent = filled === 1 ? "1 споделено име" : `${filled} споделени имена`;
        };

        const updateCharacterHint = () => {
            let hasUnexpectedCharacter = false;

            inputs.forEach((input) => {
                const shouldWarn = input.value !== "" && !nameCharacters.test(input.value);
                input.classList.toggle("has-character-warning", shouldWarn);
                hasUnexpectedCharacter ||= shouldWarn;
            });

            characterHint.hidden = !hasUnexpectedCharacter;
        };

        updateCounter();
        updateCharacterHint();

        inputs.forEach((input) => {
            input.addEventListener("input", () => {
                updateCounter();
                updateCharacterHint();
            });
        });
    });

    const form = document.querySelector("[data-poll-form]");
    const submitButton = document.querySelector("[data-submit-button]");

    if (form && submitButton) {
        const label = submitButton.querySelector("span");
        const originalLabel = label?.textContent || "Сподели предложенията";

        form.addEventListener("submit", () => {
            submitButton.disabled = true;
            if (label) {
                label.textContent = "Изпращане…";
            }
        });

        window.addEventListener("pageshow", () => {
            submitButton.disabled = false;
            if (label) {
                label.textContent = originalLabel;
            }
        });

        const firstInvalid = form.querySelector('[aria-invalid="true"]');
        firstInvalid?.focus({ preventScroll: true });
        firstInvalid?.scrollIntoView({ behavior: "smooth", block: "center" });
    }

    document.querySelectorAll("[data-statistics-group]").forEach((group) => {
        const results = Array.from(group.querySelectorAll("[data-result]"));
        const maximum = Math.max(...results.map((result) => Number(result.dataset.count) || 0), 1);

        requestAnimationFrame(() => {
            results.forEach((result) => {
                const count = Number(result.dataset.count) || 0;
                const percentage = Math.max(3, (count / maximum) * 100);
                result.style.setProperty("--bar-size", `${percentage}%`);
            });
        });
    });
})();
