# Instalar o Dwnl no Chrome — Windows x64

1. Baixe o arquivo `Dwnl-<versão>-chrome.zip` nos Assets do Release. Os arquivos automáticos `Source code` são para desenvolvimento.
2. Extraia o ZIP em uma pasta permanente, por exemplo `Documentos\Dwnl`. Mantenha essa pasta: o Chrome carrega a extensão diretamente dela.
3. Abra `extension\installer\Dwnl-Setup.exe` e conclua a instalação. Se preferir, use o `Dwnl-Setup-<versão>-windows-x64.exe` disponível separadamente no mesmo Release; é o mesmo instalador.
4. No Chrome, abra `chrome://extensions`, ative **Modo do desenvolvedor** e clique em **Carregar sem compactação**.
5. Selecione a pasta **extension**, que contém `manifest.json`.
6. Abra o Dwnl pelo menu de extensões e confira **Auxiliar conectado**. Se necessário, feche e abra o painel após instalar o auxiliar.

O instalador instala o auxiliar apenas para o usuário atual e precisa de internet para baixar versões verificadas de yt-dlp, Deno, FFmpeg e ffprobe. Não é necessário instalar Python ou esses programas separadamente. A extensão não é adicionada automaticamente ao Chrome.

O setup ainda não possui assinatura digital; o Windows pode apresentar um aviso de editor desconhecido. Os hashes dos arquivos estão em `SHA256SUMS.txt` no Release. Se o download de uma dependência falhar, confira a conexão e tente novamente; o setup verifica a integridade antes de registrar o auxiliar.

## Atualizar

Encerre as tarefas do Dwnl e feche o Chrome antes de executar o novo setup. Extraia a nova extensão em sua pasta permanente, preservando a localização usada no Chrome, e clique em **Recarregar** em `chrome://extensions`. Guarde a versão anterior separadamente se precisar voltar. O setup preserva preferências e downloads.

## Remover

Remova a extensão em `chrome://extensions` e desinstale **Dwnl private prototype** nos aplicativos instalados do Windows. Depois de remover a extensão do Chrome, sua pasta extraída pode ser apagada. Downloads já concluídos e dados locais do usuário são preservados pelo desinstalador.

## Limites desta versão

Versão experimental para Windows x64. A disponibilidade depende do site e do vídeo; não há garantia de suporte universal. Use apenas conteúdo cujo download você tenha autorização para realizar. O pacote não incorpora os executáveis de mídia dos fornecedores; eles são adquiridos durante a instalação. O antigo bundle de validação offline não faz parte desta entrega.
