# Dwnl

Protótipo pessoal para Windows: uma extensão Chrome Manifest V3 em TypeScript controla um auxiliar Go por Native Messaging. O auxiliar usa cópias privadas de yt-dlp, Deno, FFmpeg e ffprobe. Não há backend, serviço ou telemetria.

O núcleo local está implementado e revisado. A integração stdio gera mídia sintética real; os builds passaram. A limpeza transitória das fixtures NTFS recebeu uma correção limitada aos testes, verificada nos pacotes afetados. A extração online permanece desativada: o adaptador atual não consegue garantir a política de rede em todas as requisições do extrator, incluindo CDNs e redirecionamentos. Nenhuma das quatro plataformas está validada pelo produto.

## Desenvolvimento local

Ferramentas de desenvolvimento preparadas ficam em `.tools/`, com versões e hashes registrados. O arquivo de código não inclui essas ferramentas nem `node_modules`; consulte os pins em `release/dependencies.lock.json` e as versões nas evidências antes de preparar outro ambiente. Não é necessário alterar o PATH global. Binários, caches, builds e relatórios de recuperação não entram no Git.

```powershell
npm.cmd ci
npm.cmd run build:extension
& ./tools/build-host.ps1
& ./tools/build-test-harness.ps1
npm.cmd test
```

Para os testes Go, mantenha os caches no projeto:

```powershell
$env:GOCACHE = Join-Path $PWD '.tools/gocache'
$env:GOMODCACHE = Join-Path $PWD '.tools/gomodcache'
$env:GOTMPDIR = Join-Path $PWD '.tools/tmp'
$env:GOTOOLCHAIN = 'local'
$env:CGO_ENABLED = '0'
& ./.tools/go/bin/go.exe -C host test -p 1 ./... -count=1
```

Os testes de mídia usam arquivos sintéticos gerados por FFmpeg e conferidos por ffprobe. Não usam contas, cookies ou sites reais. O harness de integração mantém seus arquivos no workspace; não registra o host nem carrega a extensão em um perfil Chrome. Execute os testes Windows no contexto normal do usuário: o AppContainer pode impedir o mutex e as junctions das fixtures. `-p 1` evita concorrência entre pacotes que exercitam a regra de uma tarefa por usuário.

## Comportamento pretendido

URL colada ou aba escolhida por gesto explícito; uma tarefa ativa por usuário, sem fila. Vídeo usa a melhor opção disponível até 1080p, com preferência por MP4 H.264/AAC em qualidade equivalente. Resoluções superiores exigem escolha. MP3 usa bitrate de saída 192 kbps por padrão, com opções 128 e 320; a conversão não recupera fidelidade ausente.

O painel pode fechar sem cancelar. A perda da conexão encerra o trabalho e seus processos; o auxiliar não retoma tarefas automaticamente. Destinos são resolvidos no host, arquivos existentes são preservados e o histórico guarda até 20 tarefas. O protocolo transporta apenas JSON de controle limitado a 512 KiB.

## Artefatos e limites

O build da extensão fica em `dist/extension/`, e o auxiliar em `dist/helper/`. O pacote real compilado fica em `dist/validation-reviewed-20261013/`, rotulado **Dwnl-Validation-DO-NOT-DISTRIBUTE.exe**. O empacotamento verifica a identidade pública, os hashes, as licenças e a disponibilidade de fonte correspondente. O modo normal continua bloqueado por fonte correspondente incompleta; o artefato de validação não foi instalado nem anexado ao popup.

Não houve instalação do Dwnl, registro de Native Messaging, carregamento em Chrome, publicação ou push. A validação real de instalação/Chrome depende de autorização específica. Downloads reais dependem também de uma solução aprovada para o gate de rede e de casos cujo conteúdo e método tenham autorização adequada; DRM e controles de acesso não serão contornados.

Consulte [as evidências de validação](docs/validation/2026-10-13-prototype.md), [a especificação aprovada](docs/superpowers/specs/2026-10-13-dwnl-design.md) e [o plano](docs/superpowers/plans/2026-10-13-dwnl-implementation-plan.md). Esses documentos distinguem comportamento pretendido de resultados efetivamente comprovados.

A [retomada de 14/10](docs/validation/2026-10-14-recovery.md) registra o diagnóstico NTFS, o bloqueio exato das quatro plataformas e a prova de aquisição do FFmpeg 9.0.2 Gyan. O script `tools/probe-supplier.ps1` baixa e testa essa alternativa em `.tools/supplier-proof/`; ele requer rede, não é um instalador e não substitui as dependências produtivas.
