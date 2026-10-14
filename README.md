# Dwnl

Protótipo pessoal para Windows: uma extensão Chrome Manifest V3 em TypeScript controla um auxiliar Go por Native Messaging. O auxiliar usa cópias privadas de yt-dlp, Deno, FFmpeg e ffprobe. Não há backend, serviço ou telemetria.

O engine online usa os extratores existentes do yt-dlp através de um adaptador privado com HTTPS, DNS/IP fixados, redirects validados e bloqueio de credenciais/fallback externo. Casos reais de YouTube, Instagram, TikTok e X retornaram metadados e opções. O download testado do YouTube chegou à CDN e recebeu HTTP 403; não gerou arquivo final nem tentou contornar a recusa. Os testes de mídia sintética confirmam seleção exata, junção e MP3 por FFmpeg/ffprobe reais. Isso não promete suporte universal ou download validado em todas as plataformas.

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

O build privado da extensão fica em `dist/extension/`, e o auxiliar em `dist/helper/`. O bootstrap `dist/bootstrap-20261014-final/Dwnl-Setup.exe` contém o helper e avisos; na execução manual, adquire os quatro executáveis diretamente dos fornecedores oficiais, com URLs/versões/hashes fixados em `release/suppliers.lock.json`. Verifica arquivos antes de instalar e registrar o host. O build inclui o recurso privado no popup e a permissão `downloads` somente quando recebe `--installer` com um setup verificado. O setup compilado não foi executado.

O bundle antigo `dist/validation-reviewed-20261013/Dwnl-Validation-DO-NOT-DISTRIBUTE.exe` continua separado e bloqueado para distribuição por fonte correspondente incompleta. O bootstrap não declara fontes completas ou prontidão para distribuição pública e não embute os binários de mídia dos fornecedores.

Não houve instalação do Dwnl, registro de Native Messaging, carregamento em Chrome, publicação ou push. A validação real de instalação/Chrome depende de autorização específica. Recusas, login, DRM e desafios anti-bot interrompem a tarefa. Os resultados e os limites atuais estão na [validação online e do bootstrap](docs/validation/2026-10-14-online.md).

Consulte [as evidências de validação](docs/validation/2026-10-13-prototype.md), [a especificação aprovada](docs/superpowers/specs/2026-10-13-dwnl-design.md) e [o plano](docs/superpowers/plans/2026-10-13-dwnl-implementation-plan.md). Esses documentos distinguem comportamento pretendido de resultados efetivamente comprovados.

A [retomada de 14/10](docs/validation/2026-10-14-recovery.md) preserva o diagnóstico NTFS e o estado histórico anterior à implementação online. A aquisição Gyan 9.0.2 foi reaproveitada no novo bootstrap; o bundle antigo conserva seus próprios pins.
