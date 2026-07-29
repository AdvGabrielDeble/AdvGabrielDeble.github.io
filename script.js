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
