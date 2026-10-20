# Dwnl

<img src="docs/branding/dwnl-logo.png" width="96" height="96" alt="Logo Dwnl: seta azul de download sobre uma bandeja">

## Aplicativo Windows 0.2.0

A nova etapa oferece aplicativo Go/Wails com WebView2, fila persistente independente do navegador, bandeja e extensão auxiliar. Interface dark com azul royal. A conexão local responde antes da preparação dos componentes; a ponte usa pipe restrito ao usuário, sem servidor de rede. O modo legado/Ubuntu abaixo continua separado.

- [Instalação e uso desktop](release/INSTALL.desktop.pt-BR.md)
- [Validação e medições de 19/10](docs/validation/2026-10-19-desktop.md)
- Pacote local: `pwsh -NoProfile -File ./tools/release-desktop.ps1`
- Desenvolvimento: `pwsh -NoProfile -File ./tools/build-desktop.ps1`; use `Dwnl.exe --state-dir <pasta absoluta isolada> --bin-dir <bin privado> --destination <pasta absoluta>` para não tocar na instalação pessoal.

O build não instala, registra nem publica. Histórico e preferências legados são preservados; a retomada de downloads interrompidos exige comando explícito e reinicia a transferência nesta versão.
Protótipo para Windows e Ubuntu: uma extensão Chrome Manifest V3 em TypeScript controla um auxiliar Go por Native Messaging. No Windows, o auxiliar usa cópias privadas de yt-dlp, Deno, FFmpeg e ffprobe; no Ubuntu, yt-dlp/Deno são privados e FFmpeg/ffprobe vêm dos pacotes do sistema. Não há backend, serviço ou telemetria.

O engine online usa os extratores existentes do yt-dlp através de um adaptador privado com HTTPS, DNS/IP fixados, redirects validados e bloqueio de credenciais/fallback externo. Em 14/10, amostras públicas reais produziram vídeo Instagram e vídeo/MP3 X, confirmados por ffprobe, decodificação integral e readback de hash. YouTube e TikTok chegaram às respectivas CDNs e receberam HTTP 403; nenhum arquivo final foi gerado, e as recusas foram respeitadas. Os testes de mídia sintética também confirmam seleção exata, junção e MP3 por FFmpeg/ffprobe reais. Isso não promete suporte universal.

A [continuidade de 16/10](docs/validation/2026-10-16-completion.md) corrige prazo de análise, diagnóstico de dependências, reinstalação com estado preservado, extração ZIP no setup e ownership antes do primeiro probe. A instalação física r3 passou por inicializações sucessivas e desinstalação/reinstalação. Novos testes pela interface real no Chrome for Testing validaram vídeo Instagram, vídeo X e MP3 de 192 kbps, incluindo cancelamento e recuperação do estado do painel. Novas tentativas únicas de YouTube e TikTok retornaram `ACCESS_RESTRICTED`, sem bytes de mídia. Após recarregar a extensão, o usuário confirmou **Auxiliar conectado** no seu perfil atual do Chrome.

A [correção de YouTube](docs/validation/2026-10-16-youtube-tiktok.md) substitui o cliente obsoleto e preserva o User-Agent do extrator. O YouTube produziu vídeo completo 1080p e MP3 de 192 kbps, com hashes e decode integral conferidos no r4. A [continuidade TikTok r5](docs/validation/2026-10-16-tiktok-session.md) completa a sessão anônima autorizada em memória e preserva o áudio do próprio clipe. A extensão real concluiu vídeo H.264/AAC e MP3 de 192 kbps do TikTok, com arquivo físico, hash, journal e decode integral conferidos. O helper atual **r5 está fisicamente instalado**; setup `dist/bootstrap-20261017-r5-tiktok/Dwnl-Setup.exe`. Os testes de interface usaram Chrome for Testing isolado; o perfil pessoal permanece carregado pelo usuário. Os resultados r3/r4 acima são históricos, e os casos positivos não garantem todos os vídeos públicos.

## Pacote para GitHub Releases

Para gerar os anexos a partir deste checkout, com as ferramentas locais já preparadas:

```powershell
pwsh -NoProfile -File ./tools/release.ps1
```

O comando compila o auxiliar e o setup, gera a extensão em uma pasta separada da extensão carregada no Chrome e confere todos os arquivos dentro do ZIP por SHA-256. A saída é uma pasta nova `dist/release-<versão>-<data>/`: anexe os quatro arquivos de `assets/` ao Release e use `RELEASE-NOTES.md` como descrição. O ZIP inclui extensão, setup e instruções; o EXE separado permite instalar somente o auxiliar. O build não publica o Release nem executa a instalação. Use uma **pré-release experimental** para esta versão.

As [instruções de instalação manual](release/INSTALL.pt-BR.md) acompanham o pacote. O setup requer internet e adquire os executáveis de mídia dos fornecedores; não é o bundle offline antigo, cujo bloqueio por fontes incompletas permanece. A geração dos anexos não certifica a completude de fontes de terceiros nem altera os indicadores de distribuição dos manifests existentes.

## Ferramentas de desenvolvimento

### Ubuntu

A entrega experimental atende **Ubuntu 24.04+ x86_64 com Google Chrome .deb**. O instalador por usuário baixa fornecedores com SHA-256 fixado e registra o Native Messaging no diretório de configuração do Chrome. FFmpeg, Zenity, Python 3 e certificados são pré-requisitos dos repositórios Ubuntu. Consulte [instalação e remoção no Ubuntu](release/INSTALL.ubuntu.pt-BR.md).

Para gerar o TAR do instalador e o ZIP da extensão usando o ambiente de build Windows já preparado:

```powershell
pwsh -NoProfile -File ./tools/release-linux.ps1
```

Os anexos ficam em uma pasta nova `dist/release-linux-<versão>-<data>/assets/`. O empacotamento compila o host Linux, inclui licenças Go/x/sys, verifica todos os bytes dos arquivos comprimidos e gera `SHA256SUMS.txt`. Não publica, não instala pacotes globais e não altera a instalação Windows. As dependências Linux têm pins separados em `release/suppliers.linux.lock.json`.

O código seleciona arquivos Go por sistema operacional. O nome histórico `internal/windows` permanece como fronteira de processos/arquivos para evitar alterar todos os imports; os arquivos `*_linux.go` implementam a fronteira Linux. No Linux, cancelamento usa grupos de processos, persistência usa permissões por usuário e flock, e publicação usa linkat sem sobrescrita. Processos que escapem do grupo e encerramento abrupto do host não têm as mesmas garantias de árvore dos Job Objects Windows. Snap/Flatpak e outras arquiteturas ainda não são suportados nesta entrega.

### Windows

Ferramentas de desenvolvimento preparadas ficam em `.tools/`, com versões e hashes registrados. O arquivo de código não inclui essas ferramentas nem `node_modules`; consulte os pins em `release/dependencies.lock.json` e as versões nas evidências antes de preparar outro ambiente. Não é necessário alterar o PATH global. Binários, caches, builds e relatórios de recuperação não entram no Git.

```powershell
npm.cmd ci
npm.cmd run build:extension
& ./tools/build-host.ps1
& ./tools/bootstrap.ps1 -OutputRoot (Join-Path $PWD 'dist/bootstrap-local')
npm.cmd run build:extension -- --installer dist/bootstrap-local/Dwnl-Setup.exe
& ./tools/build-test-harness.ps1
npm.cmd test
```

Para os testes Go, mantenha os caches no projeto:

```powershell
$env:GOCACHE = Join-Path $PWD '.tools/gocache'
$env:GOMODCACHE = Join-Path $PWD '.tools/gomodcache'
$env:GOTMPDIR = Join-Path $PWD '.tools/tmp'
$env:TEMP = $env:GOTMPDIR
$env:TMP = $env:TEMP
$env:GOTOOLCHAIN = 'local'
$env:CGO_ENABLED = '0'
& ./.tools/go/bin/go.exe -C host test -p 1 ./... -count=1
```

Os testes de mídia usam arquivos sintéticos gerados por FFmpeg e conferidos por ffprobe. Não usam contas, cookies ou sites reais. O harness de integração mantém seus arquivos no workspace; não registra o host nem carrega a extensão em um perfil Chrome. Execute os testes Windows no contexto normal do usuário: o AppContainer pode impedir o mutex e as junctions das fixtures. `-p 1` evita concorrência entre pacotes que exercitam a regra de uma tarefa por usuário.

## Comportamento pretendido

URL colada ou aba escolhida por gesto explícito; uma tarefa ativa por usuário, sem fila. Vídeo usa a melhor opção disponível até 1080p, com preferência por MP4 H.264/AAC em qualidade equivalente. Resoluções superiores exigem escolha. MP3 usa bitrate de saída 192 kbps por padrão, com opções 128 e 320; a conversão não recupera fidelidade ausente.

O painel pode fechar sem cancelar. A perda da conexão encerra o trabalho e seus processos; o auxiliar não retoma tarefas automaticamente. Destinos são resolvidos no host, arquivos existentes são preservados e o histórico guarda até 20 tarefas. O protocolo transporta apenas JSON de controle limitado a 512 KiB.

## Artefatos e limites

O build privado da extensão fica em `dist/extension/`, e o auxiliar em `dist/helper/`. O bootstrap atual fica em `dist/bootstrap-20261017-r5-tiktok/Dwnl-Setup.exe` e contém o helper e avisos; adquire os quatro executáveis diretamente dos fornecedores oficiais, com URLs/versões/hashes fixados em `release/suppliers.lock.json`. Verifica arquivos antes de instalar e registrar o host. O build inclui o recurso privado no popup e a permissão `downloads` somente quando recebe `--installer` com um setup verificado. Esse setup foi executado e conferido no contexto real do usuário em 17/10, com três inicializações e preservação de dados verificadas na validação r5.

O bundle antigo `dist/validation-reviewed-20261013/Dwnl-Validation-DO-NOT-DISTRIBUTE.exe` continua separado e bloqueado para distribuição por fonte correspondente incompleta. O bootstrap não declara fontes completas ou prontidão para distribuição pública e não embute os binários de mídia dos fornecedores.

O usuário carregou a extensão no perfil atual do Chrome. A mensagem inicial "Auxiliar ausente" levou ao diagnóstico do redirecionamento do Codex; a instalação física corrigida foi conferida por hashes e caminhos finais, com registro em HKCU. Não houve publicação na Chrome Web Store. Recusas, login, DRM e desafios fora da verificação anônima TikTok autorizada interrompem a tarefa. A [validação da sessão TikTok](docs/validation/2026-10-16-tiktok-session.md) registra a continuidade atual; a [validação da instalação de 16/10](docs/validation/2026-10-16-completion.md), a [validação online de 14/10](docs/validation/2026-10-14-online.md) e o [plano anterior de instalação/Chrome](docs/validation/2026-10-14-bootstrap-chrome.md) preservam o histórico anterior às correções. A [logo](docs/branding/README.md) é incluída no painel e nos ícones do Chrome.

Consulte [as evidências de validação](docs/validation/2026-10-13-prototype.md), [a especificação aprovada](docs/superpowers/specs/2026-10-13-dwnl-design.md) e [o plano](docs/superpowers/plans/2026-10-13-dwnl-implementation-plan.md). Esses documentos distinguem comportamento pretendido de resultados efetivamente comprovados.

A [retomada de 14/10](docs/validation/2026-10-14-recovery.md) preserva o diagnóstico NTFS e o estado histórico anterior à implementação online. A aquisição Gyan 9.0.2 foi reaproveitada no novo bootstrap; o bundle antigo conserva seus próprios pins.
