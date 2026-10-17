# Fechamento do protótipo pessoal — 16/10/2026

Base de código: `278236739d9a3d06d0e3b148e44f2a4dd056820e`, branch `codex/dwnl-prototype`, com as alterações locais desta etapa ainda sem commit. O usuário autorizou concluir as pendências após a análise e escolheu seu perfil atual do Chrome para uso final. Os testes automatizados de navegador usam perfis separados no workspace.

**Correção de interpretação durante o diagnóstico:** a captura de processo `host-chrome-fast-processes.jsonl` comprovou que os primeiros comandos nominalmente dirigidos a `%LOCALAPPDATA%\Dwnl` executavam o helper em `Packages\OpenAI.Codex_2p2nqsd0c76g0\LocalCache\Local\Dwnl`. Esses primeiros resultados de instalação/readback/stdio/ciclo pertencem à árvore virtualizada do Codex, embora os processos fossem do usuário normal e `TokenIsAppContainer=false`. A instalação física foi depois realizada e verificada pelo Explorer, conforme a seção própria abaixo; os logs anteriores ficam preservados.

## Correções e regressões atuais

- Análise: o transporte espera até 120 s, cobrindo o orçamento de 90 s do motor com margem finita. Outros prazos, deduplicação e recuperação de início incerto permanecem cobertos.
- Dependências: executáveis privados ausentes em um diretório `bin` válido permitem handshake diagnóstico; execução continua restrita aos caminhos privados. Diretório ausente/inseguro, junctions e arquivos não regulares continuam recusados. O popup informa as dependências indisponíveis e orienta reinstalação, sem reutilizar afirmações históricas do gate de rede.
- Reinstalação: marcador `.dwnl-install-owner` persistente, vinculado ao produto/origem/caminho, permite reconhecer a pasta após a desinstalação sem apagar estado. Criação exclusiva e readback; recusa de marcador/manifesto alterados, registro externo, reparse, arquivos ativos e `/DIR` diferente do destino fixo.
- ZIP do setup: a primeira execução real revelou que `enhanced/nopassword` só decodifica 7z. A configuração compartilhada entre produção e teste agora usa `ArchiveExtraction=full`, necessário para os ZIPs oficiais. [Documentação do Inno](https://jrsoftware.org/ishelp/topic_setup_archiveextraction.htm).
- Segundo início: o primeiro probe criava `state/analyses` sem que o store tivesse gravado `owner.json`; a abertura seguinte recusava o diretório como estado desconhecido. O host agora inicializa ownership sob mutex e revalidação antes do probe. `Store.Open` continua somente leitura e não aceita conteúdo sem owner ou marcador inválido/estrangeiro. Diagnósticos fatais em stderr expõem somente estágio de uma lista fechada e código Win32 numérico.

Os ciclos RED/GREEN estão em `.superpowers/sdd/dwnl-completion-20261016/`, nos relatórios `extension-report.md`, `extension-diagnostics-report.md`, `host-report.md`, `installer-report.md` e `installer-extraction-report.md`.

| Verificação executada nesta etapa | Resultado |
|---|---|
| TypeScript integrado, código final | 187/187, 11 arquivos, exit 0 (`typescript-r2-full.json`). |
| Go completo final, após ownership/diagnóstico/CMD | 123 testes de topo e 158 subtestes, sem FAIL/SKIP, exit 0 (`host-r3-final-go.log`, `host-r3-final-summary.json`). Execução serial, sem builds ou mídia real concorrentes. |
| Ownership e segundo início | RED reproduziu ErrCorrupt após probe real; GREEN, rechecagem isolada e suíte completa final passaram, incluindo as recusas de conteúdo/owner desconhecidos. |
| Instalador, incluindo decoder real | 44/44: ownership Pascal, staging/pacote e dois ZIPs oficiais locais com hashes internos conferidos. |
| Typecheck e builds | Helper, harness, setup e extensão compilados, exit 0. |
| Revisão | Revisão integrada independente e releitura dos deltas finais sem bloqueadores; P2 da fixture CMD resolvido. Relatórios preservados. |
| Instalação física r3 fora do Codex | Setup exit0, 18 arquivos conferidos por hash/caminho físico, três inicializações e ciclo de desinstalação/reinstalação PASS (`outside-r3-result.json`). |
| Mídia atual no Chrome for Testing | Instagram MP4, X MKV e MP3 de 192 kbps: journal/hash/ffprobe/decode integral PASS; cancelamento, painel e histórico também passaram. |
| YouTube e TikTok atuais | Uma tentativa por URL: metadados disponíveis, transferência recusada com `ACCESS_RESTRICTED`, zero bytes. Não comprovam download funcional. |
| Perfil Chrome atual do usuário | Usuário confirmou **Auxiliar conectado** após recarregar a extensão r3 e abrir o painel pelo ícone. |

Os 16 testes Python e os downloads reais de 14/10 são evidência histórica. Seu código de transporte não mudou nesta etapa; esses testes/resultados não foram apresentados como novas execuções.

## Primeira instalação e correção da extração

No contexto normal do usuário, o baseline confirmou ausência de `%LOCALAPPDATA%\Dwnl`, registro nativo e chave de desinstalação. O primeiro setup (`7c5794f7…`) baixou os três artefatos oficiais e confirmou seus hashes, mas saiu com código 7 ao extrair Deno usando `is7zxr.dll`. A verificação pós-falha confirmou que pasta e duas chaves continuavam ausentes. Logs `setup-first-install.log`, `setup-first-result.json` e `installation-after-first-failure.json` preservam a falha.

Os dois ZIPs oficiais já disponíveis no workspace reproduziram o erro no decoder real antes da correção. Com `full/is7z.dll`, Deno e FFmpeg/ffprobe/LICENSE/README foram extraídos e conferidos por tamanho/hash. Não houve relaxamento de pins nem download de uma ferramenta alternativa.

O setup intermediário r2 fica em `dist/bootstrap-20261016-r2/Dwnl-Setup.exe`, 4.115.514 bytes, SHA256 `88aef296390b3e18ab6020278e8e968ac7a2e243387f9b7b0ba19ea6d76daaf1`. O helper correspondente tem SHA256 `ca4aa08bd70dbe91d6a2a59faa5022a454c61947aac09f4f63162bcba669faf6`. Esses artefatos foram substituídos pelo r3 descrito adiante; a extensão final contém o setup r3.

O setup corrigido foi executado no contexto normal do usuário e terminou com exit 0, mas a investigação posterior demonstrou que essa execução estava sujeita à virtualização do Codex. Nessa árvore, o readback confirmou tamanho/SHA dos 18 arquivos, manifesto stdio com origem exclusiva, registro nativo REG_SZ e chave de desinstalação. O host de produção realizou handshake com as quatro dependências disponíveis, capacidades de vídeo/MP3/adaptador verdadeiras, status vazio e exit 0 após EOF. Isso confirma o host por stdio naquela árvore, sem comprovar instalação no contexto do Chrome externo.

O ciclo com os executáveis reais de desinstalação/reinstalação, também nessa árvore virtualizada, terminou com exit 0: ambas as chaves e o manifesto foram removidos; o marcador persistente, um arquivo de estado e o arquivo de teste no destino de downloads conservaram seus hashes. A reinstalação reconheceu a pasta preservada e os 18 arquivos passaram por novo readback. Os dois arquivos de teste próprios foram removidos após a verificação; nenhuma pasta ou arquivo do usuário foi apagado. Evidências: `setup-r2-result.json`, `installation-readback-first.json`, `installed-host-smoke.json`, `installed-lifecycle.json` e `installation-readback-final.json`.

## Navegadores e perfil final

Chrome instalado: 154.0.8037.97. A ferramenta CUA não inicializa por falha do sandbox MXC (`SystemDrive`), antes de qualquer ação de interface. O teste de arraste normal da pasta via CDP público em um perfil isolado do Chrome foi recusado com `No dragged path`. Não foram usadas APIs privadas, alterações internas de perfil nem flags para contornar o bloqueio.

Foi usado o **Chrome for Testing 149.0.7827.55 já instalado**, com perfil separado, sandbox ativo e sem os flags padrão do Playwright. A extensão MV3 real foi carregada com seu ID estável. Antes da instalação do host, popup, fechamento/reabertura e reconexão confirmaram `missing-host`; o botão privado baixou o setup r2 com o SHA acima. Isso comprova onboarding no CfT, não carregamento no perfil pessoal do Chrome 154.

Após a instalação virtualizada, a conexão no CfT terminou como `disconnected`, com `Error when communicating with the native messaging host.`. A captura rápida de processo comprovou que o Chrome chamou o caminho nominal, mas executou a imagem no LocalCache do Codex, que saiu com código 1. O teste local de framing/Serve com named pipes BYTE e direitos/flags da fonte oficial do CfT passou, sem justificar alteração de produção no framing. Naquele momento a causa do exit 1 ainda não estava estabelecida; a falha de ownership descrita adiante foi depois reproduzida independentemente do Chrome. Fontes, hipóteses e limites estão em `host-chrome-report.md`. Nenhum download de mídia foi iniciado nessa tentativa.

O perfil atual do usuário não foi fechado, reiniciado nem preparado para debugging. O usuário confirmou carregamento manual da pasta `C:\Users\buddhadaiki\Documents\ChatGPT\Dwnl\.worktrees\dwnl-prototype\dist\extension` e depois a mensagem **Auxiliar ausente**. Esse rótulo corresponde à classificação `native messaging host not found`; difere do erro de comunicação no CfT. O ID esperado é `lphdonkmiecidfgjfplhdfnpphmdioeh`.

O probe somente de leitura com `PROC_THREAD_ATTRIBUTE_DESKTOP_APP_POLICY=ENABLE_PROCESS_TREE`, intermediário e descendente terminou normalmente, mas não mudou a visão física: ambos abriram a mesma imagem no LocalCache. `desktop-policy-comparison.json` registra `physicalViewDiffers=false`; nenhum setup foi executado por essa rota.

## Instalação física pelo Explorer

A cadeia COM pública do [sample Microsoft ExecInExplorer](https://github.com/microsoft/Windows-classic-samples/blob/main/Samples/Win7Samples/winui/shell/appplatform/ExecInExplorer/ExecInExplorer.cpp) foi usada para lançar um processo pelo Explorer existente, no mesmo usuário e sem elevação. O probe mostrou que a pasta física Dwnl estava ausente, enquanto o processo iniciado pelo Codex abria o helper no LocalCache (`explorer-comparison.json`). O registro nativo, contudo, era compartilhado e já existia externamente; a chave de desinstalação externa estava ausente.

Antes da instalação, foram salvos valores/tipos das duas views das chaves relevantes em `outside-install-baseline.json`. Somente a folha nativa órfã foi removida após conferir destino exato, único valor REG_SZ, ausência de subchaves e ausência da pasta física. O guard do instalador permaneceu intacto. O setup r2 com o SHA publicado acima foi então executado pelo processo do Explorer e terminou com exit0. `installation-readback-outside-first.json` confirma os 18 arquivos e registros; `outside-install-physical-paths.json` confirma cada caminho por `GetFinalPathNameByHandleW`, fora do LocalCache.

O host físico passou pelo handshake com as quatro dependências e status vazio (`installed-host-smoke-outside.json`). O ciclo físico de desinstalação/reinstalação passou e preservou marcador, estado e sentinela de download por hash (`installed-lifecycle-outside.json`). As sentinelas próprias foram removidas após a conferência. `installation-readback-outside-final.json` e `outside-install-result.json` registram o estado final, com imagem física `\\?\C:\Users\buddhadaiki\AppData\Local\Dwnl\dwnl-host.exe`. Nenhuma configuração do Codex, política, proteção, PATH global ou privilégio administrativo foi alterado.

A extensão já está carregada no Chrome atual por confirmação do usuário. Depois da instalação física r2, ele confirmou desconexão. Um novo smoke direto após o ciclo r2 também falhou, demonstrando que o problema não se restringia ao Chrome/CMD. Reutilizar um shell `require_escalated` iniciado pelo Codex não basta para excluir virtualização: verifique o caminho físico ou use a rota Explorer comprovada.

## Ownership e instalação r3

O inventário externo `outside-state-before-repair.json` mostrou somente `state/analyses` vazio, criado pelo primeiro probe às `22:30:12.1238035Z`, sem `owner.json` nem histórico. O teste `TestHostStoreReopensAfterRealDependencyProbe` reproduziu a falha após o probe real offline. A correção inicializa ownership explicitamente antes da primeira escrita de cache, sob a mesma trava e as mesmas recusas do store. Os testes também preservam arquivos e diretórios desconhecidos, owner inválido/estrangeiro e conteúdo que surge depois de `Open`.

O helper r3 tem SHA256 `732e77f426ea7cfe25fb5592c9b89a0f4d85955ef7a2ea9ccac970f7ea943a84`. O setup `dist/bootstrap-20261016-r3/Dwnl-Setup.exe` tem 4.117.573 bytes e SHA256 `356064422f80d42e6a11195618ec05524887bbf3155558abba1204184365119d`; a extensão foi reconstruída com esse recurso. O setup foi aplicado pelo Explorer com exit0. A recuperação removeu **somente** o diretório vazio inventariado, após comparar caminho físico, único filho, ausência de reparse/conteúdo e timestamp; `Directory.Delete(false)` não pode apagar arquivos recursivamente. O produto continua rejeitando esse estado desconhecido sem reparo explícito.

Passaram três execuções físicas distintas: primeiro handshake, segundo início e terceiro início após desinstalação/reinstalação. Todas confirmaram quatro dependências, capacidades, status e EOF exit0 (`installed-host-smoke-r3.json`, `installed-host-smoke-r3-repeat.json`, `installed-host-smoke-r3-after-cycle.json`). O ciclo r3 preservou o owner de estado real e a sentinela, além do marcador do instalador/download; os 18 arquivos finais passaram por hash e caminho físico (`installed-lifecycle-r3.json`, `installation-readback-r3-final.json`, `installation-r3-physical-paths.json`, `outside-r3-result.json`).

Uma tentativa da suíte Go completa r3 terminou com código `0xC0000005`, sem stack/FAIL no log, durante o teste de probe (`host-r3-go.log`). A reexecução isolada main/storage passou (`host-r3-state-recheck.log`). A causa dessa interrupção não foi estabelecida. A nova verificação completa serial, encerrada às 23:02:51Z com as fontes congeladas após o ajuste da fixture CMD, passou: 123 testes de topo e 158 subtestes, sem FAIL/SKIP e exit0. O log anterior foi preservado; não se atribui sua causa à concorrência sem evidência.

A revisão independente do código final não encontrou bloqueadores. A observação P2 sobre timeout da fixture CMD foi corrigida antes da verificação final: a árvore da fixture pertence a um Job Object antes de criar descendentes, as operações de pipe têm prazo e cancelamento e os buffers permanecem vivos até a conclusão da operação. Os testes focados de bloqueio em leitura/escrita/saída confirmaram encerramento do processo próprio e zero operações I/O pendentes. Não há encerramento por nome global de processo. Evidências: `review-state-report.md` e `host-chrome-pipe-cancellation-report.md`.

## Mídia atual pela extensão instalada

Em 16/10, o Chrome for Testing 149 executado pelo Explorer, fora da virtualização do Codex, carregou a extensão final em perfil isolado. A interface real passou por service worker MV3, Chrome Native Messaging e helper r3 fisicamente instalado, sem mocks do transporte, motor ou mídia. O perfil pessoal do Chrome 154 permaneceu aberto e sem debugging.

| Saída efetivamente criada em `C:\Users\buddhadaiki\Downloads\Dwnl` | Bytes | Verificação física |
|---|---:|---|
| Instagram, `Video by instagram.mp4` | 63.880 | H.264, 472 × 840, 4,966667 s, sem áudio; MP3 corretamente desabilitado. |
| X, `Brooklyn Nets - WATCH_ Sean Marks' full media session after our acquisition of 8-time.mkv` | 18.014.776 | H.264, 1280 × 720, áudio AAC, 324,501 s. |
| X, mesmo título com extensão `.mp3` | 7.789.131 | MP3, 192.000 bit/s, 324,545292 s. |

Fontes: `https://www.instagram.com/reel/Chunk8-jurw/` e `https://x.com/BrooklynNets/status/1349794411333394432`. Os três arquivos coincidiram com bytes e SHA256 do journal do host; o ffprobe privado leu os streams e o FFmpeg privado decodificou integralmente cada arquivo com `-xerror`, exit0 e stderr vazio. SHA256, respectivamente: `174b4814f2319c9ef4e590745363b1eb0315f58e9f5f252dcea70d5b781ea575`, `5d715fccd65db6802e39558b56f5dd9501347253d9eb9cd38f888b9d317a60e4` e `48af88c10b0c582ef5923545f96da6a0360d64249f6ce1f71132cb0a4df5c124`.

Instagram e X mantiveram os mesmos IDs de tarefa e pedido ao fechar/reabrir o painel durante download, sem duplicação. Uma tarefa X separada foi cancelada pelo botão e confirmada como `canceled`. Após fechar completamente o navegador isolado e reabri-lo, uma nova sessão do host reconciliou o MP3 concluído e preservou os quatro itens de histórico. Fechar o navegador encerrou os processos do host; a leitura final estava vazia. Evidências: `browser-r3-media-result.md`, `browser-outside-r3-media.json`, screenshots e log/exit do wrapper.

O download X durou cerca de 51 s; sua análise terminou antes do início da transferência. Esse caso **não** é evidência de análise real superior ao timeout antigo de 30 s. O prazo novo é comprovado pela regressão de transporte com relógio controlado.

Uma rodada separada fez uma análise e um único início explícito por fonte: YouTube `https://www.youtube.com/watch?v=YE7VzlLtp-4` e TikTok `https://www.tiktok.com/@scout2015/video/6718335390845095173`. Ambas forneceram metadados e recusaram a transferência com `ACCESS_RESTRICTED`, `retryable=false`, zero bytes; nenhum arquivo final foi validado. O protocolo sanitizado não expõe o status HTTP original, portanto isso **não** comprova um novo HTTP 403. Não houve nova tentativa, troca de cliente, cookie, proxy ou rota. A interface explicou a restrição e o navegador/host de teste terminaram. Evidências: `browser-r3-platforms-result.md` e `browser-outside-r3-platforms.json`.

A execução automatizada do popup usa sua página genuína da extensão; não valida sua ancoragem no ícone da barra. O usuário confirmou **Auxiliar conectado** no seu Chrome atual após recarregar a extensão r3 em `chrome://extensions` e abrir o painel pelo ícone. Essa confirmação humana sucede a resposta de desconexão do r2. Não se atribui ao perfil pessoal a rodada automatizada de mídia executada no CfT isolado.

## Entrega privada e continuidade

O código permanece no checkout `.worktrees/dwnl-prototype`, branch `codex/dwnl-prototype`, base `2782367` mais alterações locais. Não houve commit, push, publicação ou alteração do checkout `master`; o plano não rastreado preexistente na raiz foi preservado.

O diretório `dist/private-20261016-final` reúne `Dwnl-project-source.zip` (fontes atuais rastreadas e não rastreadas, documentação, testes e avisos), `Dwnl-extension.zip` (extensão final com setup r3) e `Dwnl-evidence.zip` (relatórios, logs, scripts e capturas desta etapa). Ferramentas, caches, perfis, repositório Git e arquivos de mídia do usuário não entram nesses arquivos. O `delivery-manifest.json` registra seleção, hashes, bytes e inventário de cada ZIP, incluindo conferência de cada entrada descompactada. O script usa destino novo, recusa reparse/traversal e não sobrescreve as evidências anteriores. Os arquivos são uma entrega privada, sem fontes correspondentes completas dos terceiros.

As pendências de correção, instalação e conexão desta etapa foram concluídas. Novas fontes reais exigem validação própria; a recusa de YouTube/TikTok continua sendo uma limitação atual dos casos testados. Distribuição pública exigiria trabalho separado de fontes/licenças e não está autorizada nesta etapa.

## Limites que não podem ser inferidos dos testes

As recusas HTTP 403 de YouTube/TikTok e os sucessos Instagram/X de 14/10 continuam identificados como históricos. Suporte de uma plataforma depende do caso e da resposta oferecida pelo fornecedor; nenhuma alteração de cookies, clientes, desafios ou rotas foi feita para contornar recusas.

Não foi preenchido o disco real nem provocado corte de energia. O único outro volume disponível é `G:` virtual do Google Drive/FAT32; ele não foi usado como prova de segundo volume local NTFS. Falhas desses limites continuam cobertas somente no escopo das fixtures/doubles documentados. Não há promessa de rollback transacional de todas as falhas do instalador.

Uso pessoal privado continua sendo o escopo. `publicDistributionReady=false` e `completeCorrespondingSource=false` permanecem; o bundle antigo `DO-NOT-DISTRIBUTE` não foi promovido a release.
