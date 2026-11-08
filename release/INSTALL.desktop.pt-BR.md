# Dwnl 0.4.0 — aplicativo Windows e extensão auxiliar

## Instalar

1. Execute `Dwnl-Setup.exe`. O instalador é por usuário, não exige administrador e baixa as versões verificadas de yt-dlp, Deno e FFmpeg. Precisa de internet. Ele instala o aplicativo e a extensão do Chrome juntos.
2. Abra **Dwnl** pelo menu Iniciar. Cole um link e escolha o formato; o aplicativo funciona sem extensão.
3. Para baixar direto do Chrome, clique em **Conectar ao Chrome** no aplicativo (ou em Configurações → Extensão do Chrome) e siga os passos. O Chrome só aceita extensões de fora da Chrome Web Store por um passo manual, feito uma vez: em `chrome://extensions`, ative **Modo do desenvolvedor**, clique em **Carregar sem compactação** e escolha `%LOCALAPPDATA%\Dwnl\extension`. Ao abrir o painel do Dwnl no Chrome, o aplicativo mostra a extensão como conectada. Ele só mostra isso quando a extensão falou com ele desde que foi aberto; antes disso, mostra a data da última conexão.
4. Se uma versão anterior da extensão estava carregada de outra pasta, remova-a em `chrome://extensions` antes de carregar a da pasta do Dwnl.

É necessário Microsoft Edge WebView2 Runtime (Evergreen). O setup detecta ausência antes de baixar os componentes e informa o endereço oficial de instalação: https://developer.microsoft.com/microsoft-edge/webview2/ . O runtime é compartilhado com outros aplicativos; nenhum Chromium completo é incluído no pacote.

O setup não possui assinatura digital. Compare `SHA256SUMS.txt` antes de executar. Gerar o pacote não instala nem registra o app no computador de desenvolvimento.

## Usar

- Um download por vez; outros ficam na fila persistente do aplicativo.
- Fechar a janela recolhe para a bandeja. Fechar o painel ou navegador não interrompe downloads aceitos.
- Para encerrar, use **Sair** na bandeja ou **Sair do Dwnl** nas configurações. A fila fica preservada.
- Depois de interrupção/reinício, escolha **Continuar fila**. Nesta versão a transferência interrompida reinicia do começo, com a mesma mídia/qualidade; se não for possível identificar exatamente a seleção, o app informa o erro.
- Iniciar com Windows é opcional, desligado por padrão, nas configurações.
- Downloads concluídos aparecem em **Recentes**, com **Mostrar na pasta**. Em **Ver histórico** é possível renomear o arquivo (o arquivo na pasta acompanha), apagar o registro da lista (o arquivo continua na pasta) e tentar de novo um download que falhou ou foi cancelado.
- **Limpar histórico** retira da lista os registros concluídos, cancelados ou com falha, preservando os arquivos e os recibos de idempotência no estado privado. Até 200 registros permanecem na lista atual.
- Pasta padrão: Downloads\Dwnl. Preferências, fila e recibos: `%LOCALAPPDATA%\Dwnl\state`. O histórico legado é preservado separadamente.

## Atualizar e remover

Para atualizar, abra **Configurações** e clique em **Buscar atualizações**. Se houver uma versão nova, **Atualizar** baixa o instalador do GitHub, confere os hashes e fecha o Dwnl; a instalação começa e, ao terminar, abre o Dwnl de novo. O Dwnl só consulta o GitHub quando você clica.

Também dá para executar um instalador novo por cima. Se o Dwnl estiver aberto, o instalador pede que ele feche, como **Sair**, e pergunta antes quando há um download em andamento, que volta para a fila. Se algo continuar aberto (a versão 0.3.0, que não atende a esse pedido, ou o painel do Dwnl aberto no Chrome), o instalador oferece **Fechar o Dwnl**, que encerra o que está rodando da pasta do Dwnl, ou **Tentar de novo**, depois de você usar **Sair**.

O app e a extensão são atualizados juntos, na mesma pasta. A extensão se recarrega sozinha depois que o painel fecha; se ainda mostrar a versão anterior, clique em **Recarregar** no cartão do Dwnl em `chrome://extensions`. Preserve uma cópia do diretório de estado antes de voltar à versão anterior.

Remova a extensão em `chrome://extensions` e desinstale **Dwnl** pelos aplicativos do Windows; a desinstalação apaga também a pasta da extensão. Downloads e estado do usuário são preservados. A entrada de início automático é removida somente se pertencer à mesma instalação.

## Alcance e validação

Windows x64 primeiro. Os pacotes Ubuntu e o modo legado continuam separados. Vídeos individuais, incluindo Shorts/Reels; playlists/canais não fazem parte desta etapa. Disponibilidade depende do site e vídeo.

Foi exercitado download real de Shorts, recuperação da fila após encerramento forçado, janela nativa, IPC e ponte Native Messaging em processo isolado. A instalação pessoal e o carregamento no Chrome pessoal não foram alterados. Consulte `docs/validation/2026-10-19-desktop.md`, `docs/validation/2026-11-07-startup.md` e `docs/validation/2026-11-07-updates.md` no código para medições, testes e limites da validação.
