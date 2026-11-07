O Dwnl 0.3.0 instala a extensão do Chrome junto com o aplicativo, traz um visual novo, ações no histórico e uma inicialização bem mais rápida.

## Downloads e instalação

- **Recomendado:** `Dwnl-0.3.0-windows-x64.zip`, o pacote completo com instalador, instruções e hashes internos.
- `Dwnl-Setup-0.3.0-windows-x64.exe`, o instalador sozinho. Ele instala o aplicativo e a extensão.
- `LEIA-ME-0.3.0.md`, com instalação, atualização, uso e remoção.
- `SHA256SUMS.txt`, os hashes dos arquivos distribuídos.

Requer Windows x64, internet durante a instalação e Microsoft Edge WebView2 Runtime Evergreen. O instalador não tem assinatura digital; compare `SHA256SUMS.txt` antes de executar.

**Para atualizar a partir da 0.2.x:** encerre o Dwnl pelo comando **Sair** e execute o novo instalador. A extensão agora fica em `%LOCALAPPDATA%\Dwnl\extension`: em `chrome://extensions`, remova a extensão carregada da pasta do pacote antigo e carregue a nova pasta com **Carregar sem compactação** (o app mostra o passo a passo em **Conectar ao Chrome**). A fila, o histórico e as preferências são preservados. A primeira abertura depois de atualizar verifica os componentes de novo; as seguintes ficam prontas em instantes.

## Novidades

- **Extensão incluída no instalador.** Um único download instala o app e a extensão. O cartão **Conectar ao Chrome** mostra os passos, copia o endereço e a pasta, e avisa quando a extensão se conecta.
- **Visual novo.** O app segue o tema claro ou escuro do Windows, com azul royal como destaque, e a extensão usa o mesmo visual. As qualidades aparecem como as pessoas conhecem, por exemplo "1080p Full HD · 255 MB". O `Dwnl.exe` ganhou o ícone do Dwnl.
- **Recentes e histórico.** Downloads concluídos ficam em **Recentes**, com **Mostrar na pasta**, que abre o Explorador com o arquivo selecionado, e um aviso ao concluir. No histórico dá para renomear o arquivo, apagar o registro da lista sem apagar o arquivo, e tentar de novo um download que falhou ou foi cancelado.
- **Colar e pronto.** O link é analisado assim que é colado, e há um botão de colar.
- **Animações.** Trocar de aba, a entrada de um download na fila e as mudanças nas listas são animadas. As animações são desligadas quando o Windows pede menos movimento.
- **Vídeo sempre em MP4,** com uma opção por resolução (4K, 1440p, 1080p, 720p…). H.264 é preferido, e o tamanho mostrado soma vídeo e áudio.
- **Início bem mais rápido.** Ao reabrir, o app fica pronto em cerca de 60 ms (antes, cerca de 8 s), e a busca funciona enquanto ele ainda prepara os componentes. O yt-dlp passa a ser instalado na versão *onedir* oficial, que inicia em menos de 1 s em vez de cerca de 4 s, então cada análise e cada download também começam mais rápido.
- **Mensagens de erro em português** em todo o app, inclusive as que a 0.2.1 já tinha salvado no histórico.

## Correções

- **Renomear confere o arquivo antes.** Só o arquivo baixado é movido, e uma renomeação interrompida por um encerramento é concluída na abertura seguinte.
- **Clipes do TikTok com duração arredondada** não falham mais na validação.
- **Downloads da fila e interrompidos da 0.2.x** continuam reconhecidos depois de atualizar.
- **Um componente que não inicia** é informado como componente indisponível, e a verificação seguinte refaz a checagem completa.

## Validação e limites

No Windows 11 x64, a suíte Go completa (`go test -p 1 ./... -count=1`) passou com os binários reais, incluindo o yt-dlp *onedir*. O typecheck e os testes do modelo de interface também passaram. Dos testes TypeScript, 209 passaram; 3 dependem de arquivos locais de fornecedor que não estavam na máquina de build. O instalador baixa o `yt-dlp_win.zip` 2026.08.19 e confere o hash de cada um dos 140 arquivos; a extração desse ZIP foi testada com o decodificador Inno de produção. Medições em `docs/validation/2026-11-07-startup.md`.

O instalador desta publicação foi gerado, mas não executado na máquina de build: a instalação pessoal não foi alterada. A instalação completa e o roteiro manual no Chrome ainda não foram executados com este pacote.

Continuam valendo os limites anteriores: um download interrompido recomeça do início; playlists e canais não estão incluídos; cada download tem limite de 5 GB; a instalação em Windows limpo ainda não foi validada.
