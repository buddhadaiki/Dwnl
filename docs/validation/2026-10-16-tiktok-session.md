# Sessão pública anônima do TikTok

Continuidade do [YouTube r4](2026-10-16-youtube-tiktok.md), autorizada pela resposta do usuário: **“Permitir a sessão anônima temporária”**. A autorização cobre a verificação oficial e cookies transitórios em memória, sem conta nem importação de cookies do Chrome. Os registros desta fase usam UTC e atravessam 16–17/10/2026. Base `2782367` com alterações locais; sem commit, push ou publicação.

**Estado atual r5:** auxiliar fisicamente atualizado, vídeo TikTok completo e MP3 de 192 kbps concluídos pela extensão real com arquivos/hash/journal/ffprobe/decode conferidos. A sessão usa apenas cookies anônimos temporários em memória, sem Curl em produção ou dependências novas. A validação foi feita em Chrome for Testing isolado; o perfil pessoal do Chrome não foi automatizado. Os resultados antigos de 403 abaixo são históricos.

## Histórico até a versão v4 do adaptador

A sessão nessa versão completava a verificação e obtinha metadados reais de duas publicações públicas. A transferência das mídias testadas continuava recusada pelo CDN com HTTP 403. **Não havia arquivo TikTok concluído nessa fase.** O auxiliar então instalado era o r4, cuja validação de YouTube está no documento anterior; esta seção preserva os probes de desenvolvimento anteriores ao r5.

| Prova | Resultado observado |
| --- | --- |
| Probe v2, `@scout2015/video/6718335390845095173` | Um HTTP 200, recusa local antes de metadados; sem saída de mídia. |
| Diagnóstico único v2 | Página de 1.462 bytes com estrutura WAF reconhecida. Nome opcional conhecido presente e valor vazio; validação local excessivamente estrita impediu o cálculo. Sem HTML ou valores de cookies em disco. |
| Probe v3, mesma publicação | Verificação e reextração passaram; título, duração de 10 s e 11 formatos. Primeiro GET de mídia para `v16-webapp-prime.tiktok.com`: 403, sem arquivo concluído. |
| Probe v3, `@pokemonlife22/video/7059698374567611694` | Metadados de 6 s e quatro formatos; primeiro GET ao mesmo CDN: 403, sem arquivo concluído. Amostra distinta, sem fallback do recurso recusado. |
| Probe v4, primeira publicação | A normalização do cabeçalho Host não resolveu o 403. Metadados continuam disponíveis. |
| Regressão YouTube v4 | Análise real passou: um item, 44 formatos e respostas HTTP 200; nenhum novo download de mídia solicitado. Os arquivos YouTube completos comprovados anteriormente pertencem ao r4. |

## Implementação e limites

O adaptador usa o cálculo Python finito do TikTokIE fixado em yt-dlp 2026.08.19. Há uma tentativa por processo, entrada limitada e até 1.000.001 candidatos. O jar novo e privado é compartilhado somente pelo downloader e pelo DwnlRH exclusivo. Permite apenas os nomes WAF conhecidos, no máximo dois cookies, domínio canônico `.tiktok.com`, `Secure`, descarte de sessão e validade local máxima de 120 segundos.

Somente um GET da mesma página canônica HTTPS pode receber esses cookies WAF. Não são enviados a links curtos, redirects, outros hosts ou CDNs. Credenciais fornecidas, jar externo, importação de arquivo/browser e persistência continuam bloqueados. A limpeza ocorre em sucesso, erro, interrupção e fechamento. Login, idade, conteúdo privado, DRM e respostas 401/403/429 permanecem terminais. A captura pontual do cookie anônimo oficial do CDN introduzida depois está descrita abaixo; nenhum Set-Cookie genérico é importado para o jar.

O diagnóstico real revelou que o cookie opcional pode ter nome conhecido e valor vazio. A v3 aceita esse formato, exige que o jar corresponda aos cookies efetivamente inseridos e devolve somente esses nomes para a limpeza upstream. A v4 também define a porta padrão do serializador HTTP conforme o esquema: HTTPS 443 e HTTP 80 omitem a porta padrão no Host. Testes de bytes reais provaram essa diferença anterior; a tentativa online demonstrou que ela não resolveu o 403 observado.

## Testes e revisão

- RED inicial reproduziu a recusa anterior; uma falha de preparação de TEMP/ACL está separada do RED válido.
- A revisão corrigiu duplicação de cookies por aliases de domínio e validade não inteira de objeto Cookie alterado diretamente. São invariantes do setter, sem falha live demonstrada nesse caminho.
- A regressão do opcional vazio foi reproduzida antes da correção e passou depois.
- **v4: 40 testes Python offline aprovados**, incluindo algoritmo upstream, HTTP sintético, limites, ausência de persistência, recusas e limpeza.
- **Go completo v2: 124 testes principais e 163 subtestes, zero falhas ou skips.** Após os deltas Python v3/v4, os três testes Go focados de integração/isolamento do adaptador passaram no v4. O teste com yt-dlp real é offline.
- Revisão independente das versões v2, v3 e v4 sem bloqueadores remanescentes; não representa prova de transferência TikTok bem-sucedida.

Identidade v4: `dwnl.py` SHA256 `cc0407130ab9d6b4f8105a5bc28404920f3a901ccc46732867046b9a686331ad`. Comandos, ambiente, demais hashes, logs e relatórios estão em `.superpowers/sdd/dwnl-tiktok-session-20261016/`, especialmente `host-session-freeze-v4.json`, `review.md`, `host-full-go-v2-summary.json` e `host-focused-go-v4-summary.json`.

## Investigação isolada do transporte

A inspeção estática não identificou perda de Referer, User-Agent ou assinatura de URL que explique a recusa. Também não demonstrou necessidade de cookies adicionais. Diferenças de fingerprint HTTP/TLS são hipóteses, não uma causa comprovada.

O executável fixado já inclui curl_cffi 0.16.0 e perfis de navegador. Um experimento descartável avalia um perfil e User-Agent fixos, mantendo DNS público pinado, TLS verificado, proxy desligado e o jar WAF atual. Limita metadados a 32 MiB e solicita uma única faixa de 1 KiB de uma terceira mídia pública, descartada em memória. Não altera o produto. Peer é conferido nos callbacks antes de aceitar conteúdo; não se afirma equivalência temporal com a conferência pré-HTTP do socket atual. Qualquer recusa/redirect encerra o experimento; há limite externo de 130 segundos.

A primeira execução não descobriu o plugin por falta da pasta de bundle exigida pelo yt-dlp e terminou sem relatório; a descoberta foi investigada offline. A v2 provou a importação de `DwnlCurlPocIE` pelo CLI sem URL, com um único extrator selecionado. A listagem simples `--list-extractors` retorna antes do loader nessa versão e não serve como prova de importação; essa diferença foi documentada.

A execução v2 parou no setup do cliente, antes de HTTP, sem metadados ou mídia. Uma matriz offline da biblioteca real, sem `perform` ou DNS, mostrou 22 opções aceitas e `ALTSVC_CTRL=0` recusada com CurlError 43. Alt-Svc e HSTS estão compilados na DLL: a falha é de argumento, não de recurso ausente. A v3 usa somente a máscara READONLYFILE, sem bits de protocolos alternativos nem arquivo; a segunda matriz offline aprovou as 23 opções individualmente e combinadas no mesmo handle depois do perfil Chrome146. Essas falhas de preparação não são resultados de acesso à mídia do TikTok. Relatórios em `curl-poc-v2-preparation.md` e `curl-api-setup-offline-01-report.md` distinguem os limites da inspeção estática e da API real.

O primeiro experimento Curl que efetivamente fez HTTP foi a v3, em 17/10 às 00:29 UTC, com a amostra upstream `@moxypatch/video/7206382937372134662`. Houve duas respostas HTTP 200 da página, 404.149 bytes de metadados recebidos e peers conferidos contra os IPs públicos pinados. O extrator terminou sem item e nenhum GET de mídia foi feito. A resposta não tinha observação de status lógico do item; por isso, o resultado não prova acesso ao vídeo, benefício do Curl ou causa da falha. O relatório `curl-poc-third-source-03.json` preserva essa distinção. O perfil TLS e o User-Agent foram alterados juntos; nem um sucesso isolaria TLS como causa.

Na v4 do experimento, a fonte Pokemon retornou status lógico zero, dados públicos de vídeo e quatro formatos. O único GET Range ao CDN também recebeu HTTP 403: o perfil de navegador não resolveu o bloqueio. A resposta final da página emitiu `tt_chain_token`, observado apenas como boolean; esse cookie não foi importado nem enviado. Foram aceitos zero bytes de mídia, e os 500 bytes entregues pelo callback pertencem ao corpo da resposta de erro, descartado. Evidência: `curl-poc-pokemon-04.json` e respectiva invocação, 17/10 às 01:02 UTC.

A [orientação histórica do mantenedor](https://github.com/yt-dlp/yt-dlp/issues/9997) sustenta testar a participação de cookies estabelecidos durante a extração. A presença observada do nome, isoladamente, ainda não prova essa hipótese. Um experimento posterior só poderá usar o token anônimo oficial em memória, dentro da autorização já concedida; não há motivo demonstrado para integrar Curl ao produto.

A v5 isolou essa variável: a mesma fonte, perfil, transporte e regras da v4 passaram a guardar somente o `tt_chain_token` emitido na segunda resposta canônica 200 e a enviá-lo ao único GET Range do CDN exato. Atributos, domínio, path, valor, prazo máximo de 120 segundos e proveniência passaram nas validações; o valor não foi registrado. O CDN respondeu **HTTP 206 com os 1.024 bytes solicitados**, descartados em memória. A limpeza final foi confirmada por flag. Evidência: `curl-poc-pokemon-session-05.json`, 17/10 às 01:08 UTC, com 26 fixtures offline e RED anterior preservados.

Esse resultado apoia a causa específica da sessão anônima interrompida, mas ainda não demonstra arquivo completo, MP3 ou necessidade do backend Curl. A integração em desenvolvimento conserva o transporte padrão e será validada separadamente; o auxiliar instalado continua r4 até uma instalação com readback correspondente.

## Integração r5 e primeiro vídeo completo no transporte padrão

O candidato final usa o socket padrão existente, sem Curl em produção nem dependência nova. O objeto separado `_TikTokCdnSession` recebe somente o cookie anônimo oficial `tt_chain_token` de um GET canônico HTTPS 200 durante download. Valida nome, origem, domínio `.tiktok.com`, path `/`, duplicações, atributos e tamanho; Secure é imposto localmente pelo HTTPS. A validade local é no máximo 120 segundos e pode ser reduzida pelo vencimento do servidor. O token somente é promovido após metadados públicos válidos e vinculado à URL completa exata do formato selecionado. Não entra no jar WAF, nos metadados, no protocolo ou em arquivos; análise, erro, cancelamento e fechamento o descartam. Range do mesmo URL pode usar o prazo original, sem renovação; redirects com token são terminais.

A revisão encontrou dois casos de EOF no candidato intermediário: `read(0)` e gzip com dados ainda no buffer descomprimido podiam preservar o token num fechamento prematuro. Os REDs foram observados e corrigidos; leitura gzip completa continua funcionando. A revisão final e os testes preservam essa distinção.

O probe padrão `tiktok-standard-final-video-04` já recebeu HTTP 200 de vídeo e áudio, mas terminou em `INVALID_OUTPUT` ao conferir o áudio separado. O extrator nativo oferecia a música associada de `music.playUrl` como áudio-only e, em um caminho de MP4, usava o nome do contêiner como codec. O candidato intermediário 03 normalizou esse rótulo, mas **não foi instalado nem validado com mídia real**. O candidato final 04 remove somente esse registro de música na sua procedência exata, antes da projeção dos formatos. Assim o vídeo e o MP3 usam o áudio do clipe, sem substituir voz/mixagem pela música associada. A normalização provisória foi removida; não houve relaxamento das verificações Go.

Identidade final: adaptador SHA256 `bb050734ad4245aaa354bcbe66239d6677d3c8984ac538a336f08024692cc3ce`. **77 testes Python passaram em 1,620 s**, incluindo extrator e HttpFD reais com HTTP sintético, cookie de CDN, seleção, limpeza e procedência do áudio. Parecer independente final: `host-cdn-review-final-04.md`, sem bloqueadores remanescentes. Os testes Python usam a árvore fonte pinada em Python 3.14; o teste real usa o executável yt-dlp pinado com seu Python 3.10 embutido.

A suíte Go serial teve uma falha inicial ao apagar uma fixture de armazenamento em uso, sem alteração no adaptador durante a execução. A repetição somente do pacote de armazenamento passou. A cobertura consolidada registra 124 testes principais e 163 subtestes, zero falhas remanescentes ou testes ignorados; três pacotes sem testes são identificados separadamente. Esse run precede o último ajuste Python de EOF. Os três testes Go focados passaram no hash após esse ajuste; a seleção de áudio final tem testes Python e prova real próprios. Os logs originais de falha e repetição permanecem em `host-cdn-full-go.jsonl`, `host-storage-rerun-01.jsonl` e `host-cdn-go-consolidated.json`.

Em 17/10 às 01:25 UTC, o probe `tiktok-standard-final-video-05` concluiu a mesma fonte Pokemon no transporte padrão: **MP4, 658.146 bytes, H.264/AAC, 576×1024, 5,866667 s**. SHA256 `a81ff6385b8d0c900f9a4772d238c4ab54661ed5ce60bffa816c948cf1d305f0`. O readback correspondeu à prova do engine, o ffprobe terminou em zero e a decodificação integral FFmpeg com `-xerror` terminou em zero, log vazio. O trace tem somente o formato do clipe e não solicita a faixa de música. Arquivo de desenvolvimento em `.tools/online-real/authorized-downloads/Media.mp4`; relatório `tiktok-standard-final-video-05-validation.json`.

O build r5 do auxiliar, bootstrap e extensão passou, mantendo o mesmo ID da extensão. Setup: `dist/bootstrap-20261017-r5-tiktok/Dwnl-Setup.exe`, SHA256 `5f6eb9c73896445b39eee86bff39f6f0ca5d9edb64214cb93500455a3d6d40a7`. Helper: SHA256 `273c21ec0d8020b4c97021ad99a4624153ea323b2a0bfd2648948f3878a1e347`.

## Instalação física r5

Preflight somente leitura em `r5-tiktok-preflight-01-result.json`: helper r4 inativo, instalação e destino físicos conferidos, payload anterior íntegro. A atualização autorizada passou em `r5-tiktok-upgrade-01-result.json`, concluída em 17/10 às 01:28 UTC: setup exit0, 18 arquivos do payload físico com hashes correspondentes, manifesto/registro HKCU válidos e helper final correspondente ao build. Não houve desinstalação, reparo ou exclusão de dados.

Três processos sucessivos completaram handshake e status, encerrando com EOF/exit0. Histórico, preferências, marcadores de ownership e downloads pré-existentes permaneceram iguais antes/depois da instalação e dos smokes. O caminho final foi conferido por handle como `C:\Users\buddhadaiki\AppData\Local\Dwnl`, fora do redirecionamento MSIX do Codex. O arquivo de configuração do Codex, o perfil pessoal do Chrome e o PATH global não foram alterados. Testes de mídia pela interface exigem os relatórios próprios da etapa seguinte.

## Vídeo e MP3 pela extensão instalada

A rodada `browser-tiktok-session-r5-ui-01` passou em 17/10, 01:29:15–01:29:59 UTC, no Chrome for Testing 149 isolado. Extensão, popup, service worker, Native Messaging e auxiliar físico reais, sem transporte simulado; helper/setup conferidos antes de abrir o navegador. JSON `completed`, 22 verificações, `failures=[]`, wrapper exit0. O perfil pessoal Chrome154 não foi automatizado.

| Arquivo físico em `C:\Users\buddhadaiki\Downloads\Dwnl` | Resultado |
| --- | --- |
| `Media.mp4` | 658.146 bytes; H.264/AAC, 576×1024, 5,866667 s. SHA256 `a81ff6385b8d0c900f9a4772d238c4ab54661ed5ce60bffa816c948cf1d305f0`. |
| `Media.mp3` | 141.105 bytes; MP3 **192000 bit/s**, estéreo 44100 Hz, 5,877542 s, sem vídeo. SHA256 `e39ac5063497521d353eb5608f1458bc69fbc02e89c7abfd9d0349556bd34e24`. |

Ambos correspondem em tamanho/hash ao evento de conclusão, ao journal físico e à prova preparada com `terminalConfirmed=true`; ffprobe e decode integral pelo FFmpeg instalado terminaram em zero, stderr vazio. O popup fechou e reabriu durante os dois downloads preservando `jobId`, `startRequestId` e `hostSessionId`, sem duplicação de conclusão. Capturas mostram “Auxiliar conectado” e “Concluído”. Os controles de seleção voltam ao padrão ao reabrir, mas a tarefa MP3 preservou os 192 kbps solicitados; não se afirma persistência desses controles.

O host próprio PID 44496 encerrou com seu contexto, sem novo host observado restante e sem encerrar Chrome/hosts globalmente. Relatório detalhado `.superpowers/sdd/dwnl-tiktok-session-20261016/tiktok-r5-ui-report.md`, JSON e capturas com o mesmo label. Esses resultados substituem a antiga pendência de download TikTok nessa fonte; não prometem todos os formatos ou vídeos públicos.

## Regressão final e segunda fonte pública

Depois de encerrar a suíte UI, o mesmo probe congelado (SHA256 `38bcc61b924a6e2bfb382914fe1af0df7171de72a15574d0223ec07c56f3a293`) e adaptador final foram usados serialmente, sem nova compilação ou instalação:

- **YouTube r5, análise:** `YE7VzlLtp-4` retornou um item e 43 formatos, com HTTP 200 nas páginas e no manifesto, sem novo download. Relatório `youtube-r5-final-analysis.json`, 17/10 às 01:31 UTC. Os arquivos completos YouTube vídeo 1080p e MP3 são as provas históricas r4; esta rodada confirma a análise no código r5.
- **TikTok, segunda fonte:** `@scout2015/video/6718335390845095173`, que antes recebia 403, gerou MP4 completo de **2.952.856 bytes**, H.264/AAC, 576×1024 e 10,495011 s. SHA256 `a906fa0cb9c852da371f2d9e07874e6c537e785b4e5bc61b083335dacbe5e4bf`. Hash físico correspondeu ao engine; ffprobe e decode integral FFmpeg `-xerror` terminaram em zero, log vazio. Relatórios `tiktok-r5-second-public-video.json` e `-validation.json`, arquivo de desenvolvimento em `.tools/online-real/authorized-downloads/`. O trace não solicita a trilha de biblioteca.

`final-platform-probes-invocation.json` registra tempos, fontes, hash do binário e resultados. Não houve tentativa alternativa após recusa: a primeira fonte r5 já havia passado e esta foi uma segunda amostra independente, autorizada pelo pedido de conteúdo público.

## Limites e entrega

Os casos reais cobrem duas fontes públicas TikTok, com vídeo e MP3 pela extensão instalada na primeira. A conexão do perfil pessoal foi confirmada pelo usuário na etapa anterior; a interface r5 foi automatizada em perfil isolado. Para receber os assets reconstruídos no Chrome atual, recarregar o cartão Dwnl uma vez em `chrome://extensions`.

Público não garante acesso em toda URL/data/região. Login, idade, privado, DRM, recusas HTTP e falhas de seleção permanecem terminais; o limite conservador de teste de formato/Range descrito no parecer final ainda pode afetar formatos não exercitados. Não há conta, cookie importado, persistência de sessão, proxy, provedor de tokens ou Curl habilitado no produto. Instagram/X não foram novamente baixados nesta fase; suas provas r3/r4 continuam históricas.

Entrega privada em `dist/private-20261017-tiktok-r5/`: fonte do projeto, extensão com setup incluído, evidências desta fase e `delivery-manifest.json` com hashes/entradas verificadas. Os artefatos anteriores r3/r4 continuam preservados. A entrega não inclui mídia, caches ou perfis; não há commit, push ou publicação. Distribuição pública e fontes correspondentes de terceiros permanecem pendências separadas do uso pessoal.

Os artefatos r3/r4 e seus relatórios permanecem históricos separados. Uso privado, fontes correspondentes incompletas e bloqueio de distribuição pública continuam vigentes.
