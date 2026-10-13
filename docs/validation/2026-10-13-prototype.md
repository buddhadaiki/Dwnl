# Evidências do protótipo Dwnl — 2026-10-13

Estado: núcleo implementado e revisado; builds e integração stdio local confirmados. A última suíte Go completa permanece com falha de limpeza NTFS de fixture, descrita abaixo. Este documento distingue contrato, interface controlada, processamento real de mídia local e testes em Chrome/plataformas. Uma etapa pendente não é evidência de funcionamento.

## Ambiente e isolamento

- Windows, workspace isolado em `.worktrees/dwnl-prototype`, branch `codex/dwnl-prototype`.
- Base aprovada: `971e9b7b5d4db9848bd944e78611ad71b4460222`.
- Contratos testados: commit `80096703e5f868d62f14b5838c3be0e072543b60`.
- Identidade pública: extensão `lphdonkmiecidfgjfplhdfnpphmdioeh`, host `com.dwnl.host`, protocolo 1. A chave privada de geração não foi conservada.
- Ferramentas de desenvolvimento/dependências ficam no projeto; nenhum PATH global foi alterado.
- As chaves específicas de registro do host estavam ausentes no baseline. Não houve instalação do Dwnl, registro de Native Messaging, carregamento da extensão, publicação ou push nesta etapa.

## Contratos (T1)

O corpus compartilhado contém 76 mensagens esperadas, com casos válidos e rejeições definidas manualmente. TypeScript e Go usam o mesmo corpus; testes adicionais exercitam tamanho UTF-8, contexto, expiração e metadados.

| Verificação | Resultado registrado |
|---|---|
| `npm.cmd test -- protocol` | 82/82 testes passaram após a revisão. RED inicial: 40 falhas e 34 passes; regressões Unicode tiveram falhas específicas em TS/Go antes da correção. |
| `npm.cmd run typecheck` | Passou. |
| `go -C host test ./internal/contracts ./internal/ports -count=1` | Passou; interfaces de ports não possuem testes próprios. |
| `go -C host vet ./internal/contracts ./internal/ports` | Sem diagnósticos. |
| `go -C host mod verify` | Todos os módulos verificados. |

Esses resultados comprovam validação do contrato. Processamento, processos e integração têm evidências próprias abaixo.

Revisão independente T1 aprovada, sem achados bloqueadores. O único achado P3 foi uma diferença no tratamento de texto Unicode em branco; corrigido no commit `9974234b6ea63f543dbaa85453e5de8773740d08`, com dois casos comuns e GREEN em ambas as linguagens. Nenhum campo ou interface foi alterado.

## Ferramentas privadas verificadas

| Ferramenta | Versão | Evidência |
|---|---|---|
| Go | 1.27.2, Windows amd64 | Arquivo oficial de go.dev, SHA256 publicado e execução da versão. |
| yt-dlp | 2026.08.19 | SHA256 publicado, digest do asset e assinatura GPG do checksum; Python 3.10.11/EJS 0.8.0 embutidos. |
| Deno | 2.9.7 | SHA256 publicado do arquivo/binário e execução da versão. |
| FFmpeg/ffprobe | N-127511-g01cf1a5396-20261013 | SHA256 publicado do arquivo; build GPL, versão e commit de origem registrados. |
| Inno Setup | 7.1.0 | Pacote oficial com hash e Authenticode válidos; compilador privado preparado no modo portátil oficial. |

O smoke test de ferramentas gerou 0,1 segundo de áudio sintético em MP3 e o ffprobe confirmou codec, duração, sample rate e canais. Isso não substitui a validação do motor do produto.

Licenças e referências de fonte fixadas foram capturadas. A coleta contém 126 arquivos oficiais de fonte, com cerca de 1,04 GB e hashes conferidos; seis arquivos principais de versões/commits exatos foram fixados no pacote como fonte parcial. A correspondência completa dos componentes nativos, transitivos e avisos ainda não foi estabelecida. `completeCorrespondingSource=false` permanece correto.

## Extensão e interface (T2)

- 50 testes de estado, popup e transporte passaram, além dos 82 de protocolo. A revisão independente aceitou as correções de rejeição de início e de acesso à aba enquanto outra operação está pendente.
- O service worker possui a conexão Native Messaging. O popup recebe snapshots e sua reabertura não solicita um segundo início.
- A prévia usa os módulos de produção com respostas sintéticas explicitamente identificadas. Sete cenários foram renderizados e exercitados em Edge headless: 1080p padrão, MP3 192 kbps, escolha explícita de 2160p, falta de áudio, rede bloqueada, auxiliar ausente e largura de 460 px. Capturas foram inspecionadas; nenhum erro JavaScript da aplicação ocorreu.
- A tentativa de usar o navegador integrado falhou ao preparar os assets do kernel; a verificação visual usou Playwright com perfil temporário próprio, fechado após o teste. Não foi carregada a extensão em Chrome.

O build emite sete módulos locais, com Manifest V3 e identidade estável; exclui testes e a prévia. Nenhum instalador utilizável foi anexado ao popup. O estado de auxiliar ausente informa essa limitação.

## Host, processos e arquivos (T3)

O host valida a origem exata e o framing binário de Native Messaging. Uma tarefa ativa por usuário é protegida por mutex. A perda do canal cancela o trabalho; processos são criados suspensos e associados atomicamente a um Windows Job Object antes de executar.

Os dez testes de processos usaram executáveis de fixture próprios. A correção de uma corrida real retém handles dos processos pertencentes ao job e só confirma cancelamento depois de sua sinalização e de contagem ativa zero. O teste preservou um processo não relacionado; também comprovou encerramento dos filhos após saída abrupta do host. A repetição focada de cancelamento passou dez vezes. Falhas de confirmação resultam em `CANCEL_FAILED` e preservam o estado que pode ainda estar em uso.

A publicação usa o handle do arquivo e a identidade do diretório aprovado. Testes reais verificaram Unicode, colisão e sufixo, substituição do arquivo, substituição do diretório, hardlink e junction. A limpeza remove apenas o objeto cuja identidade foi capturada e preserva arquivos substitutos. Um teste posterior confirmou que conclusão física verificada prevalece sobre cancelamento ou desconexão tardios.

No sandbox AppContainer, a criação de junction e o mutex de usuário exigem contexto normal do usuário. Os testes correspondentes usam execução pontual autorizada; não houve alteração de ACL, política de segurança ou antivírus. Os testes não executam o host de produção contra as pastas reais do usuário.

A revisão identificou também persistência prévia à publicação, preferência de pasta e observação de EOF com mensagens pendentes. Essas correções foram implementadas, testadas e aceitas pela revisão independente: a preparação durável precede cada tentativa de publicação; a reconciliação verifica identidade de arquivo/diretório, tamanho e hash, sem reiniciar o motor ou regravar o histórico; a preferência de pasta permanece privada e recebe novo ID opaco em cada host. Arquivos substitutos com os mesmos bytes não são confirmados quando há prova de identidade preparada.

Os testes simulam a janela de crash omitindo o registro terminal após publicar um arquivo real. Não houve corte de energia ou término forçado do host de produção nessa janela. Um guard central passou por RED/GREEN ao rejeitar bytes UTF-8 inválidos no ledger e na preferência. A revisão fechou os seis achados anteriores e esse sétimo achado, sem encontrar outro defeito de código no escopo autorizado.

## Processamento de mídia (T4)

Fixtures sintéticas de dois segundos, com flash e beep, foram processadas por FFmpeg real e integralmente decodificadas. O vídeo usa `-map 0:v:0 -map 1:a:0 -c copy`; a invocação comprova streamcopy. A diferença medida entre flash e beep foi **19,955 ms**, abaixo dos 100 ms previstos. A duração passou a tolerância `max(0,5 s, 1%)`.

| Saída física registrada nos testes | Bytes | SHA256 |
|---|---:|---|
| MP4 H.264/AAC | 5843 | `a42b07945a4aa2ef8ce2dcdf740c087c3da2aaf8bc9d6f2fec90d0cd90211dc4` |
| MP3 128 kbps | 32645 | `4ac2c7a9bbe88666d945135d734f86c3c8b053461103aae2d3db6651ea444f82` |
| MP3 192 kbps | 48946 | `517982bcca77df515225e5601f3674f1c7432adad3055ce9a7dd1bd39054bb61` |
| MP3 320 kbps | 81547 | `b77cd15bb16e60ca22726bea94c346811cce804b311a2d9abe6569029aa6dc33` |

O ffprobe confirmou codec, streams e bitrate configurado. Os arquivos de teste são temporários; esses hashes registram a leitura física, sem representar downloads disponibilizados ao usuário. O código das fixtures é reproduzível.

Após as correções de publicação e prova física, 28 testes de motor e seus subcasos passaram em 68,480 s, com `go vet` sem diagnóstico. Falta de encoder, disco cheio e término não confirmado foram exercitados por doubles na fronteira de processo; não se afirma que o disco real foi preenchido. Não houve teste físico em um segundo volume.

## Empacotamento (T5)

Nove testes verificam arquivos/controlos próprios de empacotamento. A revisão independente aceitou a comparação dos bytes copiados contra os hashes fixados, os hashes de entrada do compilador e a gravação UTF-8 do manifesto em caminhos Unicode.

O Inno 7.1.0 compilou o payload real de validação, depois do rebuild do host. O arquivo **Dwnl-Validation-DO-NOT-DISTRIBUTE.exe** tem **251.436.131 bytes**, SHA256 **60adc9ef4ac1991e2e532ca80a784705f9ad379993ca3e2b79fc7465baf14462**. Os 26 arquivos do payload passaram por nova leitura e comparação de hashes. Nenhum instalador foi executado. Não há `Dwnl-Setup.exe` utilizável no popup: o modo normal rejeita fonte correspondente incompleta, e esse artefato permanece explicitamente não distribuível.

A primeira compilação real parou nas verificações de ferramentas. O diagnóstico reproduziu perda da flag de versão quando o array PowerShell de um elemento se tornava escalar. A chamada usa agora `string[]` explícito. Um segundo problema era o MP3 de 0,1 s não detectável automaticamente pelo ffprobe desse build; o smoke usa um segundo, mantendo autodetecção, codec e duração. O teste real das quatro dependências passou e conferiu MP3 CBR 192 kbps; nenhuma verificação foi desativada. `.gitattributes` também preserva os bytes de avisos/licenças fixados por SHA256, com RED/GREEN no checkout Windows e sem mudar a configuração global do Git.

O instalador foi desenhado por usuário, em HKCU, sem serviço, autostart ou PATH global. A lógica de desinstalação exige ownership do manifesto e do valor de registro; preserva downloads e extensão e recusa arquivos ativos. Isso tem testes de código/arquivos controlados, sem prova de instalação ou desinstalação real.

O [levantamento de alternativas de dependências](../research/2026-10-13-dependency-delivery-options.md) avalia um bootstrap único com downloads públicos fixados por hash ou um build menor de FFmpeg com fontes acessíveis. Nenhuma opção foi implementada e nenhum gate foi alterado. O cache exato do fornecedor retornou 401; não foram solicitadas credenciais ou tentado contorno.

## Integração e plataformas

Três testes stdio passaram em **14,37 s após o último rebuild**: cliente TypeScript de produção conversando com Go, framing fragmentado/status/gate de rede e perda deliberada do ACK de início, com repetição idempotente, MP3 192 kbps real, size/hash físicos e reconciliação após reabrir o ledger. Usaram um harness explicitamente de teste e diretórios próprios; o executável não integra o instalador. Não equivalem a testes em Chrome.

A amostra preservada dessa integração é `Dwnl-demo-synthetic-192kbps.mp3`: **37.034 bytes**, SHA256 **129c83d0befc623281d37fe6967b2eb4b20430b9671190557ac93edb278e4b9b**. O ffprobe confirmou MP3, 192.000 bps e 1,541208 s. O áudio é sintético; não veio de uma plataforma.

## Checkpoint final e pendência de testes

Código de produto congelado em `60d3e07cf6148b811230b39cd6dde7e22043bc6b`; o commit posterior de documentação apenas registra os resultados. Host Windows amd64/CGO desabilitado: **3.969.024 bytes**, SHA256 **c96949dec59484e439e3fd07597bab03559b5d3211801d29557be0d37e9680e6**. A extensão compilada contém sete módulos; seu ZIP tem **24.489 bytes**, SHA256 **462e75d8c04f445102b98faaaf0a05fd94f0ed6079e4812596d6b0cec71d7890**.

| Escopo | Resultado observado |
|---|---|
| TypeScript: contrato e extensão | 132 testes passaram. |
| Empacotamento controlado | 9 testes passaram. |
| Integração stdio reconstruída | 3 testes passaram, 14,37 s. |
| Dependências privadas reais | 1 teste passou, 5,48 s; quatro versões/hashes e MP3 CBR. |
| Typecheck e builds | Passaram; extensão, host e harness compilados. |
| Go completo em `401ab76` | 94 testes de topo passaram em seis pacotes. |
| Go completo após o guard UTF-8 | Exit 1: uma falha em limpeza `TempDir`, 94 testes de topo passaram; cinco pacotes passaram, storage falhou. |
| Regressões específicas do guard UTF-8 | Ledger e preferência passaram, exit 0, 0,656 s. |
| Revisão independente | Sete achados fechados; nenhum achado de código remanescente. A revisão conserva a pendência da suíte Go. |

Na última suíte Go, `TestPublishRejectsReplacedIntermediateWithoutMovingIt` passou nas verificações de preservação e falhou em `testing.go:1619`, durante `TempDir RemoveAll`, com “diretório não vazio”. O problema também apareceu em outras fixtures de substituição; uma inspeção posterior encontrou um diretório vazio e nenhum processo Go ativo. A causa não foi isolada. Um overlay que alterava apenas nomes de arquivos sintéticos não resolveu e não foi aplicado às fontes. Não houve reescrita da limpeza de produção, relaxamento de asserts, exclusão ampla ou alteração de segurança. **A última suíte Go não está integralmente aprovada**; o pass anterior não substitui esse resultado.

Os relatórios locais em `.superpowers/sdd/dwnl-prototype` preservam comandos, ciclos RED/GREEN, ownership e checkpoints recuperáveis.

| Plataforma | Situação | Motivo |
|---|---|---|
| YouTube | Não validada | Nenhum caso real executado pelo produto. |
| Instagram | Não validada | Nenhum caso real executado pelo produto. |
| TikTok | Não validada | Nenhum caso real executado pelo produto. |
| X | Não validada | Nenhum caso real executado pelo produto. |

O gate de rede exige restringir as requisições efetivas do motor, CDNs, redirecionamentos e endereços conectados. Validar somente a URL inicial não basta. Enquanto isso não estiver demonstrado, operações reais de extração permanecem bloqueadas; mídia sintética local pode validar processamento sem acesso a sites.

Chrome real, instalação/desinstalação, bootstrap do recurso privado e registro do host dependem de autorização específica para o ambiente de teste. Não serão executados implicitamente por build ou teste.
