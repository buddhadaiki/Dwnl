# Prévia controlada do popup

Depois do build `node tools/build-extension.mjs`, sirva a raiz do repositório por um servidor estático de desenvolvimento e abra `/extension/dev/preview.html`.

A página usa o HTML/CSS e os módulos de produção do popup com um transporte sintético identificado na barra superior. Permite explorar vídeo/MP3, qualidades acima de 1080p, dimensão desconhecida, falta de áudio, bloqueio de extração, auxiliar ausente/incompatível, cancelamento e fechar/reabrir o painel. Nenhum instalador, host ou arquivo de mídia é executado ou criado.

O build exclui `dev/` e `tests/`. Essa prévia comprova somente a interface diante das respostas controladas; não comprova instalação, ciclo de vida MV3 ou extratores reais.
