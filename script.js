// Configuração central. Substituir pelo número oficial do escritório, com DDI e DDD.
// Exemplo: 5553999999999
const WHATSAPP_NUMBER = "5553999529993";

function whatsappUrl(message) {
  return `https://wa.me/${WHATSAPP_NUMBER}?text=${encodeURIComponent(message)}`;
}

// Atualiza todos os links de WhatsApp a partir de data-message.
document.querySelectorAll(".js-whatsapp").forEach((link) => {
  const message = link.getAttribute("data-message") || "Olá, gostaria de atendimento previdenciário.";
  link.setAttribute("href", whatsappUrl(message));
});

const form = document.getElementById("leadForm");
if (form) {
  form.addEventListener("submit", function (event) {
    event.preventDefault();

    const data = new FormData(form);
    const nome = data.get("nome")?.toString().trim() || "Não informado";
    const cidade = data.get("cidade")?.toString().trim() || "Não informado";
    const situacao = data.get("situacao")?.toString().trim() || "Não informado";
    const relato = data.get("relato")?.toString().trim() || "Não informado";

    const mensagemPadrao =
      "Olá, gostaria de atendimento previdenciário sobre benefício por incapacidade. Preenchi a triagem rápida da página e gostaria de encaminhar meu caso para análise inicial.";

    const dadosFormulario = [
      "DADOS INFORMADOS NO FORMULÁRIO:",
      `Nome completo: ${nome}`,
      `Cidade/UF: ${cidade}`,
      `Situação: ${situacao}`,
      `Relato inicial: ${relato}`,
    ].join("\n");

    const autorizacao =
      "Autorizo o contato pelo WhatsApp e o uso das informações enviadas para avaliação inicial do atendimento jurídico. Sei que a análise final depende da revisão do advogado responsável e que não há promessa de resultado.";

    const message = `${mensagemPadrao}\n\n${dadosFormulario}\n\n${autorizacao}`;

    window.open(whatsappUrl(message), "_blank", "noopener,noreferrer");
  });
}

const prefersReducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

if (!prefersReducedMotion) {
  const revealCandidates = document.querySelectorAll(
    ".hero__copy, .hero-panel, .trustbar__inner, .card, .split > *, .steps li, .section-heading, .proof-box, .form-grid > *, .faq details, .final-cta__box"
  );

  revealCandidates.forEach((element, index) => {
    element.classList.add("reveal");

    if (element.classList.contains("hero-panel") || element.matches(".form-grid > :last-child")) {
      element.classList.add("reveal--right");
    }

    if (element.matches(".split > :first-child") || element.matches(".form-grid > :first-child")) {
      element.classList.add("reveal--left");
    }

    if (element.classList.contains("card") || element.matches(".steps li")) {
      element.classList.add("reveal--scale");
      const groupIndex = Array.from(element.parentElement.children).indexOf(element);
      element.style.setProperty("--reveal-delay", `${Math.min(groupIndex * 70, 280)}ms`);
    } else {
      element.style.setProperty("--reveal-delay", `${Math.min(index * 18, 160)}ms`);
    }
  });

  const revealObserver = new IntersectionObserver(
    (entries, observer) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting) {
          entry.target.classList.add("is-visible");
          observer.unobserve(entry.target);
        }
      });
    },
    { threshold: 0.14, rootMargin: "0px 0px -8% 0px" }
  );

  revealCandidates.forEach((element) => revealObserver.observe(element));
}

const topbar = document.querySelector(".topbar");
if (topbar) {
  const updateTopbar = () => {
    topbar.classList.toggle("is-scrolled", window.scrollY > 8);
  };
  updateTopbar();
  window.addEventListener("scroll", updateTopbar, { passive: true });
}


const backToTop = document.querySelector(".back-to-top");
if (backToTop) {
  const updateBackToTop = () => {
    backToTop.classList.toggle("is-visible", window.scrollY > 520);
  };

  updateBackToTop();
  window.addEventListener("scroll", updateBackToTop, { passive: true });

  backToTop.addEventListener("click", () => {
    window.scrollTo({ top: 0, behavior: prefersReducedMotion ? "auto" : "smooth" });
  });
}
