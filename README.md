# Dwnl

<img src="docs/branding/dwnl-logo.png" width="96" height="96" alt="Logo Dwnl: d com seta de download em verde-lima e grafite">

Protótipo pessoal para Windows: uma extensão Chrome Manifest V3 em TypeScript controla um auxiliar Go por Native Messaging. O auxiliar usa cópias privadas de yt-dlp, Deno, FFmpeg e ffprobe. Não há backend, serviço ou telemetria.

O engine online usa os extratores existentes do yt-dlp através de um adaptador privado com HTTPS, DNS/IP fixados, redirects validados e bloqueio de credenciais/fallback externo. Em 14/10, amostras públicas reais produziram vídeo Instagram e vídeo/MP3 X, confirmados por ffprobe, decodificação integral e readback de hash. YouTube e TikTok chegaram às respectivas CDNs e receberam HTTP 403; nenhum arquivo final foi gerado, e as recusas foram respeitadas. Os testes de mídia sintética também confirmam seleção exata, junção e MP3 por FFmpeg/ffprobe reais. Isso não promete suporte universal.

A [continuidade de 16/10](docs/validation/2026-10-16-completion.md) corrige prazo de análise, diagnóstico de dependências, reinstalação com estado preservado, extração ZIP no setup e ownership antes do primeiro probe. A instalação física r3 passou por inicializações sucessivas e desinstalação/reinstalação. Novos testes pela interface real no Chrome for Testing validaram vídeo Instagram, vídeo X e MP3 de 192 kbps, incluindo cancelamento e recuperação do estado do painel. Novas tentativas únicas de YouTube e TikTok retornaram `ACCESS_RESTRICTED`, sem bytes de mídia. Após recarregar a extensão, o usuário confirmou **Auxiliar conectado** no seu perfil atual do Chrome.

A [correção de YouTube](docs/validation/2026-10-16-youtube-tiktok.md) substitui o cliente obsoleto e preserva o User-Agent do extrator. O YouTube produziu vídeo completo 1080p e MP3 de 192 kbps, com hashes e decode integral conferidos no r4. A [continuidade TikTok r5](docs/validation/2026-10-16-tiktok-session.md) completa a sessão anônima autorizada em memória e preserva o áudio do próprio clipe. A extensão real concluiu vídeo H.264/AAC e MP3 de 192 kbps do TikTok, com arquivo físico, hash, journal e decode integral conferidos. O helper atual **r5 está fisicamente instalado**; setup `dist/bootstrap-20261017-r5-tiktok/Dwnl-Setup.exe`. Os testes de interface usaram Chrome for Testing isolado; o perfil pessoal permanece carregado pelo usuário. Os resultados r3/r4 acima são históricos, e os casos positivos não garantem todos os vídeos públicos.

## Desenvolvimento local

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
