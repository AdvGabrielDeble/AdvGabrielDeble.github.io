"use strict";

const WHATSAPP_NUMBER = "555331975015";

const whatsappMessages = {
  incapacidade:
    "Olá, meu benefício por incapacidade foi negado, cessado ou está demorando. Gostaria de organizar meu caso para análise.",
  geral:
    "Olá, gostaria de atendimento sobre uma situação previdenciária. Não tenho certeza de qual benefício ou caminho jurídico se aplica ao meu caso.",
};

function whatsappUrl(message) {
  return `https://wa.me/${WHATSAPP_NUMBER}?text=${encodeURIComponent(message)}`;
}

document.querySelectorAll("[data-whatsapp]").forEach((link) => {
  const type = link.dataset.whatsapp;
  const message = whatsappMessages[type];

  if (message) {
    link.href = whatsappUrl(message);
  }
});

const formSection = document.getElementById("formulario");
const situationField = document.getElementById("situacao");
const reportField = document.getElementById("relato");

function updateReportPlaceholder() {
  if (!reportField || !situationField) return;

  reportField.placeholder =
    situationField.value === "Não sei qual benefício se aplica"
      ? "Conte o que aconteceu, se já houve pedido no INSS e qual é a tua principal dificuldade hoje."
      : "Ex.: fiz perícia, o benefício foi negado, ainda estou em tratamento e não consigo voltar ao trabalho.";
}

document.querySelectorAll("[data-route]").forEach((button) => {
  button.addEventListener("click", () => {
    if (situationField) {
      situationField.value = button.dataset.route || "";
      updateReportPlaceholder();
    }

    formSection?.scrollIntoView({ behavior: "smooth", block: "start" });
  });
});

situationField?.addEventListener("change", updateReportPlaceholder);

document.getElementById("lead-form")?.addEventListener("submit", (event) => {
  event.preventDefault();

  const form = event.currentTarget;

  if (!(form instanceof HTMLFormElement) || !form.reportValidity()) {
    return;
  }

  const data = new FormData(form);
  const name = String(data.get("nome") || "").trim();
  const city = String(data.get("cidade") || "").trim();
  const situation = String(data.get("situacao") || "").trim();
  const report = String(data.get("relato") || "").trim();

  const message = [
    "Olá, gostaria de encaminhar minha situação previdenciária para triagem.",
    "",
    `Nome: ${name}`,
    `Cidade/UF: ${city}`,
    `Situação informada: ${situation}`,
    `Relato breve: ${report}`,
    "",
    "Gostaria que o escritório analisasse os fatos e identificasse o benefício ou encaminhamento jurídico adequado.",
  ].join("\n");

  window.open(whatsappUrl(message), "_blank", "noopener,noreferrer");
});

function setupPremiumInteractions() {
  const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  const topbar = document.querySelector(".topbar");

  const progress = document.createElement("div");
  progress.className = "scroll-progress";
  progress.setAttribute("aria-hidden", "true");
  document.body.prepend(progress);

  let ticking = false;

  function updateScrollState() {
    const scrollTop = window.scrollY || document.documentElement.scrollTop;
    const maxScroll = Math.max(
      document.documentElement.scrollHeight - window.innerHeight,
      1,
    );
    const progressScale = Math.min(scrollTop / maxScroll, 1);

    progress.style.transform = `scaleX(${progressScale})`;
    topbar?.classList.toggle("is-scrolled", scrollTop > 14);
    ticking = false;
  }

  function requestScrollUpdate() {
    if (!ticking) {
      window.requestAnimationFrame(updateScrollState);
      ticking = true;
    }
  }

  updateScrollState();
  window.addEventListener("scroll", requestScrollUpdate, { passive: true });
  window.addEventListener("resize", requestScrollUpdate);

  const revealTargets = document.querySelectorAll([
    ".section-heading",
    ".card",
    ".route-box",
    ".route-card",
    ".steps li",
    ".proof-box",
    ".lead-form",
    ".faq details",
    ".final-cta__box",
    ".trustbar span",
    ".form-assurance",
  ].join(","));

  revealTargets.forEach((element, index) => {
    element.dataset.reveal = "";
    element.style.transitionDelay = `${Math.min(index % 6, 5) * 55}ms`;
  });

  if (reduceMotion || !("IntersectionObserver" in window)) {
    revealTargets.forEach((element) => element.classList.add("is-visible"));
    return;
  }

  const observer = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
        }
      });
    },
    {
      rootMargin: "0px 0px -10% 0px",
      threshold: 0.12,
    },
  );

  revealTargets.forEach((element) => observer.observe(element));
}

setupPremiumInteractions();
