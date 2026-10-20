# Dwnl 0.2.0 — aplicativo Windows e extensão auxiliar

## Instalar

1. Extraia este pacote em uma pasta permanente.
2. Execute `Dwnl-Setup.exe`. O instalador é por usuário, não exige administrador e baixa as versões verificadas de yt-dlp, Deno e FFmpeg. Precisa de internet.
3. Abra **Dwnl** pelo menu Iniciar. Cole um link e escolha o formato; o aplicativo funciona sem extensão.
4. Para ações no navegador, abra `chrome://extensions`, ative **Modo do desenvolvedor**, clique **Carregar sem compactação** e escolha a pasta `extension` deste pacote.
5. Se a extensão anterior já estava carregada, atualize-a para esta pasta/versão e recarregue. Use o app e a extensão 0.2.0 juntos: a ponte antiga usa outro protocolo.

É necessário Microsoft Edge WebView2 Runtime (Evergreen). O setup detecta ausência antes de baixar os componentes e informa o endereço oficial de instalação: https://developer.microsoft.com/microsoft-edge/webview2/ . O runtime é compartilhado com outros aplicativos; nenhum Chromium completo é incluído no pacote.

O setup não possui assinatura digital. Compare `SHA256SUMS.txt` antes de executar. Gerar o pacote não instala nem registra o app no computador de desenvolvimento.

## Usar

- Um download por vez; outros ficam na fila persistente do aplicativo.
- Fechar a janela recolhe para a bandeja. Fechar o painel ou navegador não interrompe downloads aceitos.
- Para encerrar, use **Sair** na bandeja ou **Sair do aplicativo** nas configurações. A fila fica preservada.
- Depois de interrupção/reinício, escolha **Continuar fila**. Nesta versão a transferência interrompida reinicia do começo, com a mesma mídia/qualidade; se não for possível identificar exatamente a seleção, o app informa o erro.
- Iniciar com Windows é opcional, desligado por padrão, nas configurações.
- O histórico oferece abrir o arquivo e sua pasta. **Arquivar histórico** retira registros concluídos/cancelados/falhos da lista, preservando arquivos e recibos de idempotência no estado privado. Até 200 registros permanecem na lista atual.
- Pasta padrão: Downloads\Dwnl. Preferências, fila e recibos: `%LOCALAPPDATA%\Dwnl\state`. O histórico legado é preservado separadamente.

## Atualizar e remover

Encerre o Dwnl pelo comando Sair antes de atualizar. O instalador recusa substituir arquivos em uso; não mata processos. Atualize app e extensão juntos. Preserve uma cópia do diretório de estado antes de voltar à versão anterior.

Remova a extensão em `chrome://extensions` e desinstale **Dwnl** pelos aplicativos do Windows. Downloads e estado do usuário são preservados. A entrada de início automático é removida somente se pertencer à mesma instalação.

## Alcance e validação

Windows x64 primeiro. Os pacotes Ubuntu e o modo legado continuam separados. Vídeos individuais, incluindo Shorts/Reels; playlists/canais não fazem parte desta etapa. Disponibilidade depende do site e vídeo.

Foi exercitado download real de Shorts, recuperação da fila após encerramento forçado, janela nativa, IPC e ponte Native Messaging em processo isolado. A instalação pessoal e o carregamento no Chrome pessoal não foram alterados. Consulte `docs/validation/2026-10-19-desktop.md` no código para medições, testes e limites da validação.
