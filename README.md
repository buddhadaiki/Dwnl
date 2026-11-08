# Dwnl

<img src="docs/branding/dwnl-logo.png" width="96" height="96" alt="Logo Dwnl: seta azul de download sobre uma bandeja">

Baixe vídeos e músicas do YouTube, Instagram, TikTok e X no Windows. Cole o link, escolha a qualidade em MP4 ou o MP3 e pronto. Uma extensão opcional do Chrome permite baixar direto da aba aberta.

Tudo roda no seu computador: sem conta, sem servidor e sem telemetria. O Dwnl só fala com os sites dos links que você cola e, quando você pede, com o GitHub para buscar atualizações.

## Baixar

Baixe a versão mais recente em **[Releases](https://github.com/buddhadaiki/Dwnl/releases/latest)**. O pacote `Dwnl-<versão>-windows-x64.zip` traz o instalador, as instruções e os hashes.

Requisitos:

- Windows 64 bits (testado no Windows 11).
- Internet durante a instalação: o instalador baixa yt-dlp, Deno e FFmpeg dos fornecedores oficiais e confere cada arquivo.
- [Microsoft Edge WebView2 Runtime](https://developer.microsoft.com/microsoft-edge/webview2/), que já vem no Windows 11. O instalador avisa se faltar.

O instalador não tem assinatura digital, então o Windows pode mostrar um aviso. Compare o `SHA256SUMS.txt` da release antes de executar.

## Instalar

1. Execute `Dwnl-Setup.exe`. A instalação é só para o seu usuário e não pede administrador.
2. Abra o **Dwnl** pelo menu Iniciar.
3. Opcional: para baixar pelo Chrome, clique em **Conectar ao Chrome** no app e siga os passos. O Chrome só aceita extensões de fora da loja por um passo manual, feito uma vez.

## Usar

- Cole um link: ele é analisado na hora. Escolha a qualidade do vídeo (MP4) ou o MP3 (128, 192 ou 320 kbps) e clique em **Baixar**.
- Um download por vez; os outros esperam na fila, que continua salva mesmo se o app fechar.
- Os arquivos vão para `Downloads\Dwnl`. Os concluídos aparecem em **Recentes**, com **Mostrar na pasta**.
- Em **Ver histórico** dá para renomear um arquivo, tirar um registro da lista (o arquivo fica na pasta) ou tentar de novo um download que falhou.
- Fechar a janela deixa o Dwnl na bandeja. Para encerrar, use **Sair** na bandeja.
- Se o computador desligar no meio de um download, abra o app e clique em **Continuar fila**.

## Atualizar e remover

Para atualizar, abra **Configurações** e clique em **Buscar atualizações**. Se houver versão nova, **Atualizar** baixa o instalador, confere os hashes, fecha o Dwnl e abre ele de novo já atualizado. Rodar um instalador novo por cima também funciona: ele fecha o Dwnl sozinho e pergunta antes se houver um download em andamento. Fila, histórico e preferências são mantidos, e a extensão se recarrega sozinha.

Para remover, desinstale o **Dwnl** em Configurações → Aplicativos do Windows e remova a extensão em `chrome://extensions`. Seus downloads continuam na pasta.

Mais detalhes no [guia de instalação e uso](release/INSTALL.desktop.pt-BR.md).

## Limites

- Só vídeos individuais, incluindo Shorts e Reels; playlists e canais não.
- Vídeos que exigem login, têm DRM ou são bloqueados pelo site não são baixados: o Dwnl avisa e para.
- Um download interrompido recomeça do início.
- Cada download tem limite de 5 GB.
- A disponibilidade depende de cada site e de cada vídeo.

## Para desenvolvedores

O app é Go com [Wails](https://wails.io/) e WebView2 (`host/cmd/dwnl-app`). A extensão é Chrome Manifest V3 em TypeScript (`extension/`) e fala com o app por Native Messaging, através de um pipe local restrito ao usuário. A mídia passa por cópias privadas de yt-dlp, Deno, FFmpeg e ffprobe, com versões e hashes fixados em [`release/suppliers.lock.json`](release/suppliers.lock.json).

O Go e o Inno Setup ficam em `.tools/`, sem mexer no PATH global; o Node vem do sistema. Os builds não instalam nada.

```powershell
npm.cmd ci
pwsh -NoProfile -File ./tools/build-desktop.ps1     # Dwnl.exe e a ponte em dist/desktop
pwsh -NoProfile -File ./tools/release-desktop.ps1   # setup, LEIA-ME, hashes e ZIP em dist/desktop-release-*
npm.cmd run typecheck
npm.cmd test
```

Para rodar sem tocar na instalação pessoal: `Dwnl.exe --state-dir <pasta isolada> --bin-dir <bin privado> --destination <pasta>`.

Testes Go, com os caches dentro do projeto. Use `-p 1`, porque os pacotes exercitam a regra de uma tarefa por usuário, e rode no contexto normal do usuário:

```powershell
$env:GOCACHE = Join-Path $PWD '.tools/gocache'
$env:GOMODCACHE = Join-Path $PWD '.tools/gomodcache'
$env:GOTMPDIR = Join-Path $PWD '.tools/tmp'
$env:TEMP = $env:GOTMPDIR; $env:TMP = $env:TEMP
$env:GOTOOLCHAIN = 'local'; $env:CGO_ENABLED = '0'
& ./.tools/go/bin/go.exe -C host test -p 1 ./... -count=1
```

Defina `DWNL_MEDIA_BIN` com uma pasta que tenha os quatro executáveis para rodar também os testes com binários reais.

Medições e evidências ficam em [`docs/validation/`](docs/validation/), por exemplo a [inicialização mais rápida](docs/validation/2026-11-07-startup.md) e o [app desktop](docs/validation/2026-10-19-desktop.md). Há também uma versão experimental para Ubuntu 24.04+ com o host legado: veja [a instalação no Ubuntu](release/INSTALL.ubuntu.pt-BR.md) e `tools/release-linux.ps1`.

## Créditos

Desenvolvido por Gabriel Souza, em um processo assistido por Claude (Anthropic) e Codex (OpenAI).
