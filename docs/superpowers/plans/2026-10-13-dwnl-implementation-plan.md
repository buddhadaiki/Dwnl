# Dwnl — Implementation Plan

> **For agentic workers:** Use superpowers-local:subagent-driven-development para a execução coordenada recomendada. As tarefas usam checkboxes para acompanhamento. Este documento é somente plano; aguardar a escolha de execução pelo usuário.

**Goal:** Entregar um protótipo pessoal Windows em que a extensão analisa uma URL/aba escolhida e permite salvar vídeo ou MP3 com qualidade, progresso e cancelamento pelo auxiliar local.

**Architecture:** A extensão MV3 controla a interface e uma conexão Native Messaging. Um auxiliar Go executa yt-dlp/Deno/FFmpeg privados e controla processos, estado e arquivos. Um instalador por usuário entrega dependências e registra somente o host Dwnl em HKCU.

**Tech Stack:** TypeScript, HTML/CSS e APIs Chrome, sem framework de interface; tsc para compilação; Vitest e testes de DOM para lógica/interface; Go com biblioteca padrão e golang.org/x/sys/windows para o host; go test para testes; Inno Setup para o instalador. Playwright, apenas como ferramenta de teste de extensão em perfil isolado, quando disponível/autorizado. Nenhum desses componentes será instalado nesta etapa.

**Spec:** docs/superpowers/specs/2026-10-13-dwnl-design.md, commit 971e9b7. Especificação aprovada pelo usuário em 2026-10-13 às 20:12:38 UTC. Cópia na Library: libfile_06b288e5b414819183598c2c8a3fff4e.

**Estado do repositório:** contém somente a especificação; nenhum código, build ou teste de produto existe. Node v24.16.0 está disponível; Go e ISCC não foram encontrados no PATH na inspeção. Preparar as ferramentas de desenvolvimento na fase de execução, com versões fixadas e sem alterar configurações globais automaticamente.

## Global Constraints

- Uso pessoal no Windows; YouTube, Instagram, TikTok e X são alvos de validação, sem garantia universal. Somente casos públicos cujo conteúdo e método tenham autorização adequada; sem contas, cookies, DRM ou contorno de bloqueios.
- URL colada ou aba atual por gesto; uma tarefa ativa por usuário Windows, sem fila ou lote automático.
- Vídeo: melhor resolução disponível até 1080p por padrão; preferir MP4 H.264/AAC em qualidade equivalente, sem redução silenciosa ou aumento artificial. Se só houver resoluções superiores, exigir escolha.
- MP3: bitrate de saída constante 192 kbps; opções 128 e 320 kbps. Sem promessa de ganho de fidelidade.
- Downloads/Dwnl configurável por diálogo nativo; sem sobrescrever arquivos existentes.
- Fechar painel não cancela; fechar Chrome/perder conexão interrompe; sem reinício automático. Histórico local: últimas 20 tarefas, apagável sem remover downloads.
- Mensagem nativa: no máximo 512 KiB por item; só JSON de controle, nunca bytes de mídia. Código da extensão empacotado, sem eval ou carregamento remoto de código.
- Auxiliar/registro por usuário em HKCU, ID estável da extensão e allowed_origins exato; sem backend nosso, serviço, autostart, porta de rede ou telemetria.
- Binários privados, versões/hashes/licenças registrados. yt-dlp.exe inclui Python/EJS; Deno e FFmpeg/ffprobe são incluídos pelo pacote. Não instalar runtime global para o usuário.
- Fixtures: tolerância de duração max(0,5 segundo, 1%); sincronização A/V até 100 ms. Mocks não comprovam extratores reais.
- Neste turno: somente este plano e sua entrega na Library. Sem produto, downloads de binários, instalação, registro, publicação, push ou execução de testes de produto.

## Review Focus

1. Só existem formatos acima de 1080p ou faltam dimensões: não selecionar qualidade inventada — teste T2.
2. Confirmação de start se perde: status pelo requestId evita duplicação; outra sessão não retoma — teste T3.
3. Cancelamento durante MP3 e encerramento abrupto: filhos encerram; outro FFmpeg permanece — T3/T6.
4. Destino em outro volume e nome já existente: preservar arquivo anterior e não confirmar parcial — T4/T6.
5. Fixture passa, mas site real pede login/bloqueia ou encoder falta: distinguir capacidade local, restrição e suporte não validado — T4/T6.

## Organização e contratos comuns

Os caminhos abaixo são arquivos futuros, não arquivos criados por este plano.

| Dono | Caminhos | Responsabilidade |
|---|---|---|
| Coordenador, T1 | package.json, package-lock.json, tsconfig.json, .gitignore; contracts/protocol.v1.md, contracts/cases.json, contracts/extension-identity.json; extension/src/protocol.ts, extension/tests/protocol.test.ts; host/internal/contracts/types.go, types_test.go, host/internal/ports/engine.go; host/go.mod, host/go.sum | Contrato, corpus comum, identidade pública, tipos/portas e ferramentas de teste. |
| Extensão, T2 | extension/manifest.json, extension/popup.html, extension/popup.css; extension/src/controller.ts, popup.ts, state.ts; extension/tests/state.test.ts, popup.test.ts | Permissões, Native Messaging, interface, cache e transporte simulado somente de teste. |
| Host, T3 | host/cmd/dwnl-host/main.go; host/internal/native/, runtime/, storage/, windows/ e seus arquivos *_test.go | Framing, sessão, tarefas, registro local, exclusão, Job Object e destinos. |
| Motor, T4 | host/internal/engine/ytdlp.go, formats.go, media.go, network.go, arquivos *_test.go; host/testdata/ | Extração, seleção, invocação permitida, junção/MP3, rede e saída. |
| Instalador, T5 | installer/dwnl.iss, native-host.json.template; release/dependencies.lock.json, THIRD_PARTY_NOTICES.md; tools/package.ps1; installer/tests/package.test.ts | Dependências verificadas, payload, host HKCU e desinstalação. |
| Integração, T6 | tools/build-extension.mjs, build-host.ps1; tests/integration/native.test.ts, chrome.spec.ts; docs/validation/2026-10-13-prototype.md; README.md | Builds locais, integração e relato da evidência real. |

O coordenador é o único que altera arquivos comuns, lockfiles e Git. Agentes não ajustam o contrato unilateralmente. Builds e executáveis gerados ficam fora do Git. Dependências de desenvolvimento/motor recebem versões exatas quando obtidas e verificadas; não preencher hashes ou versões fictícios neste plano.

### Interfaces fixadas em T1

- Wire protocol v1: solicitações com protocolVersion, requestId, operation, payload e hostSessionId após handshake. Respostas têm kind=response, requestId, hostSessionId e result ou error. Eventos kind=event têm hostSessionId, jobId, sequence, state e progresso opcional.
- Operações: handshake, analyze, listFormats, chooseDestination, start, status, reconcile e cancel, com entradas/resultados da especificação. status aceita jobId ou requestId de início; reconcile consulta somente registros anteriores e nunca reinicia trabalho.
- Tipos comuns: Request, Response, JobEvent, Analysis, MediaItem, VideoChoice, Progress, DownloadPlan e CompletedFile; modos video/mp3, bitrates 128/192/320 e estados equivalentes aos estados de produto da especificação. O contrato define nomes JSON exatos e códigos de erro antes da delegação.
- TypeScript: decodeMessage(value: unknown): Response | JobEvent; reduceState(state: AppState, message: Response | JobEvent): AppState; NativeClient.request(operation, payload, requestId): Promise<Response>; NativeClient.subscribe(listener): unsubscribe.
- Go: Engine.Analyze(context.Context, string) (Analysis, error); Engine.Execute(context.Context, DownloadPlan, func(Progress)) (CompletedFile, error). O serviço Session.Handle(context.Context, Request) Response emite eventos por callback separado.
- A porta ProcessRunner.Run(context.Context, ExecSpec, func(ProcessLine)) (ProcessResult, error) é interna ao host, nunca um payload do navegador. ExecSpec contém papel do executável conhecido e argumentos construídos pelo motor; o runner resolve caminhos privados e controla a árvore.
- ProcessLine identifica stdout/stderr e uma linha limitada; ProcessResult informa exitCode e se houve cancelamento. Essas estruturas também são definidas em T1, sem admitir comandos ou caminhos pela interface.
- CompletedFile contém caminho final verificado, formato, tamanho e hash. DownloadPlan resolve destinationId no host e captura a seleção validada; a interface não fornece caminhos de executável ou flags.
- Identidade de release: host com nome com.dwnl.host, chave pública e extensionId correspondentes, protocolo v1 e manifesto de dependências. Chave privada fora do Git; ID não dependerá do caminho local da pasta nem de upload à Web Store.

## T1 — Contratos e base verificável

**Dependência:** nenhuma. **Responsável:** coordenador. **Checkpoint:** ambos os validadores aceitam/rejeitam o mesmo corpus, e o usuário pode ler o exemplo de tarefa/progresso.

**Arquivos/interfaces:** criar os caminhos T1 da tabela e os testes extension/tests/protocol.test.ts e host/internal/contracts/types_test.go. Produz todos os tipos/portas acima; não registra host nem implementa extratores.

- [ ] Preparar somente as ferramentas de desenvolvimento autorizadas, fixar versões/lockfiles e estabelecer comandos npm.cmd test, npm.cmd run typecheck e go -C host test. Falha de ferramenta/rede não conta como RED.
- [ ] Escrever primeiro DecodeValidHandshake, RejectUnknownOperation, RejectOversizedFrame e RejectStartOutsideAnalysis: assertar mesmos campos/códigos no corpus, limite 524288 bytes e ausência de paths/flags livres.
- [ ] Rodar npm.cmd test -- protocol e go -C host test ./internal/contracts; registrar RED por comportamento ainda ausente.
- [ ] Implementar os tipos, decodificação estrita, constantes e interfaces mínimas; escrever contrato v1 e identidade pública estável local. Não implementar corpos do motor.
- [ ] Repetir os testes e npm.cmd run typecheck; exigir GREEN e compatibilidade TS/Go. Uma revisão factual do contrato precede as frentes paralelas.

## T2 — Extensão visível com transporte controlado

**Dependência:** T1. **Responsável:** agente da extensão. **Checkpoint:** popup navegável com análise/qualidades/modos/progresso simulados claramente identificados; nenhuma alegação de download real.

**Consome:** NativeClient, decodeMessage e tipos T1. **Produz:** controlador nativo MV3 e interface HTML/CSS; reduceState implementado em state.ts. O transporte de teste fica fora do pacote de entrega.

- [ ] Escrever primeiro CurrentTabOnlyOnGesture, ChooseBestAtMost1080, RequireChoiceIfOnlyAbove1080, Mp3192DefaultAndNoAudioDisabled, IgnoreOldSequence e UnknownProgressHasNoFakePercent. Asserções incluem 128/192/320, 20 registros e preservação do estado ao fechar a interface.
- [ ] Rodar npm.cmd test -- state popup e confirmar RED para cada comportamento que ainda falta.
- [ ] Implementar T2: colar/usar aba, analisar, escolher item, vídeo/MP3, pasta aprovada, iniciar/cancelar, snapshot e mensagens de erro. service worker possui uma única conexão; popup é cliente dela.
- [ ] Testar ausência/incompatibilidade do auxiliar, evento duplicado, confirmação perdida e cache ativo sem confirmação; permitir cancelamento enquanto os processos encerram, sem antecipar “Cancelado”.
- [ ] Rodar npm.cmd test -- state popup e npm.cmd run typecheck; conferir visualmente o popup em harness de teste. Permissões: nativeMessaging, storage, activeTab; downloads somente se recurso instalador existir. Sem hosts amplos/content script permanente.

## T3 — Auxiliar, sessão e árvore de processos

**Dependência:** T1. **Responsável:** agente do host. **Checkpoint:** diálogo stdio real com processo local de teste, snapshot, idempotência e cancelamento; ainda sem Chrome/registro ou sites reais.

**Consome:** tipos/Engine/ProcessRunner T1, Engine falso em testes. **Produz:** Session.Handle, processo Native Messaging e ProcessRunner Windows. Registro próprio resolve destinationId, status e reconcile.

- [ ] Escrever primeiro FrameSplitAcrossReads, StartIdempotentByRequestId, BusyAcrossConnections, ReconcileOldSessionReadOnly, CorruptLedgerDoesNotClaimSuccess e ClearHistoryKeepsDownloads.
- [ ] Rodar go -C host test ./internal/native ./internal/runtime ./internal/storage e confirmar RED.
- [ ] Implementar framing stdio limitado, origem exata, sessão, exclusão por usuário Windows, cache/registro mínimo e diálogo de pasta. Persistir somente dados permitidos; não retomar registros anteriores.
- [ ] Escrever CancelKillsAllOwnedChildrenButNotOtherFfmpeg, EofKillsJob e CleanupRejectsExternalJunction. Implementar Job Object com kill-on-close e associação antes de liberar a execução do filho; encerramento confirmado antes do terminal cancelado.
- [ ] Rodar go -C host test ./internal/native ./internal/runtime ./internal/storage ./internal/windows. Exercitar processos reais de teste e diretórios próprios; registrar GREEN/limitações. Não escrever HKCU nesta tarefa.

## T4 — Motor e mídia, com gate de rede

**Dependência:** T1; usar ProcessRunner falso até T3 estar integrado. **Responsável:** agente do motor. **Checkpoint:** arquivos locais de fixture viram vídeo combinado e MP3 reproduzíveis; fontes reais continuam não validadas.

**Consome:** DownloadPlan, Progress, CompletedFile e ProcessRunner. **Produz:** implementação Engine em ytdlp.go; tradutores em formats.go; finalização em media.go; política demonstrável em network.go.

- [ ] Escrever primeiro ParseAvailableFormats, StaleSelectionRejected, NoAudioRejectsMp3, KnownExecutableAndArgsOnly, PublicHostRedirectToPrivateRejected e MissingMp3EncoderIsCapabilityError. Rodar go -C host test ./internal/engine e registrar RED.
- [ ] Fazer uma prova curta da restrição de rede: comprovar protocolo/domínio/endereço também nas requisições, CDNs e redirecionamentos do motor. Rejeitar URL inicial não basta. Se a integração com yt-dlp não garantir o requisito, bloquear uso real dessa capacidade e relatar a limitação; não introduzir servidor/proxy/driver novo como solução silenciosa.
- [ ] Implementar os adaptadores de entrada das quatro plataformas, leitura estruturada do yt-dlp, análise paginada, seleção por IDs e argumentos fechados. Isolar configurações/plugins/cookies; parar em restrição de acesso. Usar dependências privadas verificadas, sem downloads silenciosos.
- [ ] Escrever e executar FixturesVideoStreamcopy, Mp3128192320, DestinationOtherVolumeNoOverwrite e FinalEventLostReconcilesWithoutRestart. Implementar somente o processamento/finalização necessários; usar ffprobe, hash e registro próprios. Confirmar streamcopy pela invocação efetiva, não apenas pela aparência da saída.
- [ ] Rodar go -C host test ./internal/engine; testar fixture A/V e MP3 com binários reais autorizados. Verificar reprodução, codec/streams, bitrate configurado, tolerâncias da especificação, falhas de disco/destino e limpeza. Não declarar plataforma validada por esses testes.

## T5 — Instalador e pacote privado

**Dependência:** T1; script/template pode avançar com payload de teste, mas pacote utilizável depende de T3/T4. **Responsável:** agente do instalador. **Checkpoint:** pacote local com instalador real, manifesto, dependências, licenças e instruções; nenhuma instalação executada automaticamente.

**Consome:** extensionId/chave pública/nome do host, binário compilado, dependencies.lock.json com versões/hashes/capacidades reais. **Produz:** Dwnl-Setup.exe e pacote local da extensão, sem URL pública.

- [ ] Escrever primeiro PackageRejectsMissingBinaryOrWrongHash, OriginMatchesPublicIdentity, NoticesCoverBundledBinaries e UninstallManifestOwnsOnlyDwnlPaths. Rodar npm.cmd test -- package e registrar RED.
- [ ] Implementar dwnl.iss por usuário, PrivilegesRequired=lowest, manifesto stdio/allowed_origins exato e registro HKCU. Não exigir Python/Deno global, modificar PATH, instalar serviço ou configurar Chrome.
- [ ] Implementar tools/package.ps1 para verificar payload/arquitetura, copiar binários e avisos, invocar ISCC e montar pacote. yt-dlp.exe é combinado GPLv3+; FFmpeg depende do build; cumprir fontes/avisos correspondentes. Falha de licença/hash/capacidade interrompe empacotamento.
- [ ] Incluir instalador como recurso privado da extensão, sem web_accessible_resources e sem ciclo: o instalador contém host/dependências; não contém outra cópia da extensão. O botão usa recurso existente; se o Chrome não o transferir, orientar abertura do instalador no pacote local.
- [ ] Rodar npm.cmd test -- package e compilação ISCC somente quando as ferramentas forem disponibilizadas. Marcar payload de teste como não distribuível. Não executar installer/uninstaller nem escrever registro neste checkpoint.

## T6 — Integração e validação real

**Dependência:** T2–T5 e gate de rede T4. **Responsável:** coordenador; uma revisão independente do conjunto. **Checkpoint:** demonstração real do fluxo que passou, com matriz explícita do que está restrito/indisponível/não validado.

**Consome:** todas as interfaces e artefatos entregues. **Produz:** pacote integrado, README e docs/validation/2026-10-13-prototype.md com evidências, sem publicação.

- [ ] Rodar os testes TS/Go integrados e build-extension/build-host. Build Go para Windows com CGO desabilitado; ferramentas privadas não entram no Git. Confirmar contrato/framing real entre TS e Go antes de registrar o host.
- [ ] Escrever primeiro IntegrationLostAckNoDuplicate, PanelReconnectSameJob, ChromeCloseInterrupts, CancelDuringConversion e FinalizeOtherVolume. Rodar com host de teste e confirmar RED; integrar T2–T4 e obter GREEN.
- [ ] Após autorização específica para executar/carregar/registrar em ambiente de teste, usar perfil Chrome isolado e instalação por usuário. Testar sem dependências globais, ID em outra pasta, bootstrap, handshake, reabertura/interrupção, árvore de processos e desinstalação. Se não houver ambiente limpo/segundo volume, registrar o teste real pendente; simulação não o substitui.
- [ ] Selecionar casos reais públicos autorizados de YouTube/Instagram/TikTok/X. Para cada um: analisar, salvar vídeo, gerar MP3 quando houver áudio, conferir formato/streams/reprodução e registrar URL ou identificador seguro, data, versões e resultado. Interromper em login/DRM/controle de acesso sem contorno.
- [ ] Entregar o relato local/Library, com estados **Validada para o caso**, **Não validada**, **Restrita** ou **Indisponível**. Fazer uma revisão independente do diff e da evidência do conjunto; corrigir achados concretos. Não anunciar quatro plataformas funcionando sem quatro evidências reais adequadas.

## Cobertura da especificação

| Critérios | Responsável e prova final |
|---|---|
| A01, A02, A06, A17 | T2 → T6: interface/estado controlado, depois Chrome real. |
| A05, A07, A08, A09, A10, A18 | T1/T3/T4 → T6: protocolo, processos e filesystem, depois interrupções reais. |
| A03, A04, A16 | T4 → T6: processamento de fixtures, restrições e casos reais autorizados. |
| A12, A13, A14 | T5 → T6: pacote conferido, depois instalação/bootstrap/desinstalação real. |
| A11 | T1/T2/T3/T4: origem, payload, URL/rede, argumentos e caminhos; gate de rede obrigatório. |
| A15 | T6: matriz por plataforma com arquivos reais; nenhum mock satisfaz esse critério. |

## Execução coordenada recomendada

Recomendo continuar nesta sessão com subagentes locais, após o usuário escolher a execução. O coordenador entrega T1 e congela o contrato; agentes diferentes assumem T2, T3, T4 e T5 com propriedade exclusiva dos caminhos. T3/T4 usam portas falsas enquanto os componentes reais não estiverem prontos; T5 só produz pacote utilizável após receber payload real. O coordenador integra T6.

Manter lockfiles/Git somente com o coordenador e evitar alterações compartilhadas concorrentes. Preservar modelo/esforço configurados, sem mudar preferências globais. Uma revisão independente do contrato e uma revisão factual por frente entregue, seguida da revisão do conjunto integrado, substituem auditorias especulativas ou redesenho recorrente. Commits locais de execução dependem da autorização aplicável; este plano não faz commit de produto.

Cada checkpoint mostra ao usuário o artefato e qual nível de evidência existe: contrato, simulação, fixture, integração Chrome ou plataforma real. TDD registra teste RED, alteração mínima e GREEN; falha de ferramenta ou site não é sucesso de teste nem justificativa para burlar restrições.

## Riscos e bloqueios a relatar

- **Toolchains:** Go/ISCC ainda precisam ser disponibilizados para compilar. A fase de planejamento não os instala. Confirmar Windows/arquitetura e registrar versões, checksums e licenças antes de preparar o pacote.
- **Rede do extrator:** o enforcement exigido pela especificação ainda não foi demonstrado. A prova T4 decide a viabilidade dentro deste desenho; falha bloqueia uso real e é reportada antes de acrescentar arquitetura.
- **Sites reais:** faltam casos autorizados e resultados. Bloqueios de login, mudanças de extrator ou condições de uso podem limitar plataformas mesmo que o auxiliar/processamento funcione.
- **Ambiente e artefato:** perfil/Windows limpos, segundo volume, bootstrap de recurso privado, avisos do Windows e desinstalação só poderão ser confirmados na validação real autorizada.

## Revisão única de consistência

- Cobertura: A01–A18 atribuídos, incluindo MP3, lifecycle, temporários, instalação e evidência real.
- Interfaces: campos JSON e tipos definidos em T1; T2/T3/T4 consomem as mesmas portas. Agentes não dependem de arquivos que ainda não têm dono.
- Dependências: T2–T5 paralelizáveis após T1; pacote final e integração aguardam componentes reais. Não confundir payload falso com release.
- Proporção: seis tarefas e checkpoints, sem framework de interface, backend, serviço ou investigação extensa nova.
- Evidência: comandos deste plano são futuros; nenhum teste/build/instalação foi executado. Riscos pendentes têm gate ou relato explícito.

## Referências de stack

- [Go: ferramentas Windows](https://go.dev/doc/install)
- [Go: APIs Windows](https://pkg.go.dev/golang.org/x/sys/windows)
- [Inno Setup](https://jrsoftware.org/isinfo.php)
- [Inno: instalação sem privilégios administrativos](https://jrsoftware.org/ishelp/topic_setup_privilegesrequired.htm)

O usuário revisa este plano e escolhe a execução antes de qualquer implementação. A aprovação da especificação permitiu elaborar o plano, não executar os instaladores ou alterar registro/configurações.
