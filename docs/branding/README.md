# Logo Dwnl

O símbolo combina um **d** minúsculo com uma seta para baixo. As cores seguem o painel: grafite esverdeado `#252B28` e verde-lima `#D7F484`.

- `dwnl-logo.png`: imagem mestre PNG de 1254 × 1254, com transparência fora do quadrado arredondado.
- `../../extension/assets/icons/icon-{16,32,48,128}.png`: tamanhos usados pelo Chrome e pelo painel.

A imagem mestre foi gerada em 17/10/2026 com a ferramenta integrada `image_gen`, sem referências externas. Os ícones foram apenas redimensionados com filtro Lanczos, preservando o canal alfa. O build da extensão copia os PNGs automaticamente; a chave e o ID da extensão permanecem os mesmos.

Brief usado na geração: símbolo único de aplicativo para Dwnl; seta de download dominante com sugestão de **d** minúsculo; geometria arredondada, traços grossos, leitura a 16 px, verde-lima e grafite, fundo externo transparente, sem texto ou logos de plataformas.

Validação de 17/10/2026: build com o instalador r5 verificado concluído, sete módulos compilados e ID `lphdonkmiecidfgjfplhdfnpphmdioeh` preservado. Os 37 testes de `popup.test.ts` e `popup-transport.test.ts` passaram. O Chromium de teste renderizou o painel de prévia e os quatro ícones, sem erros de página; tamanhos e hashes dos PNGs copiados foram conferidos. Essa verificação visual usou transporte sintético e não iniciou downloads de mídia. Relatório local ignorado: `.tools/ui-qa/logo-20261017/report.json`.
