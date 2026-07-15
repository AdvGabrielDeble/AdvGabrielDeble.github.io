# LP Escritório — ajuste pontual v13

## Alteração realizada

Foi alterado o número de destino do WhatsApp da triagem e dos CTAs da LP para:

**+55 53 3197-5015**

Formato técnico aplicado:

```text
555331975015
```

## Arquivos alterados

1. `script.js`
   - Alterado `WHATSAPP_NUMBER` de `5553999529993` para `555331975015`.
   - Este é o ponto principal que controla a mensagem da triagem rápida enviada pelo formulário.

2. `index.html`
   - Atualizados os links estáticos `https://wa.me/5553999529993` para `https://wa.me/555331975015`.
   - Isso evita inconsistência caso algum botão seja clicado antes do JavaScript atualizar os links.

## O que não foi alterado

- Design.
- Textos institucionais.
- Estrutura da LP.
- Fotos.
- Estilos CSS.
- Política de privacidade.
- Assets.

## Como publicar

Substituir apenas estes arquivos no repositório/site publicado:

- `index.html`
- `script.js`

Manter todos os demais arquivos da versão-base atual.
