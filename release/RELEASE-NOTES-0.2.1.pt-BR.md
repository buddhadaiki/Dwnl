O Dwnl 0.2.1 corrige problemas da fila, da ponte com a extensão e do histórico encontrados após a 0.2.0. Não há recursos novos.

## Downloads e instalação

- **Recomendado:** `Dwnl-0.2.1-windows-x64.zip` — pacote completo com instalador, extensão, instruções e hashes internos.
- `Dwnl-Setup-0.2.1-windows-x64.exe` — instalador separado do aplicativo.
- `Dwnl-0.2.1-extension.zip` — extensão para carregar sem compactação no Chrome.
- `LEIA-ME-0.2.1.md` — instalação, atualização, uso e remoção.
- `SHA256SUMS.txt` — hashes dos quatro arquivos distribuídos.

Para atualizar a partir da 0.2.0, encerre o Dwnl pelo comando **Sair**, execute o novo instalador e recarregue a extensão a partir da pasta `extension` do pacote. A fila, o histórico e as preferências são preservados. O protocolo entre app e extensão não mudou, mas atualize os dois juntos para receber todas as correções.

Requer Windows x64, internet durante a instalação e Microsoft Edge WebView2 Runtime Evergreen. O instalador não tem assinatura digital; compare `SHA256SUMS.txt` antes de executar.

## Correções

- **Download cancelado não volta mais para a fila.** Cancelar e, em seguida, sair do app (ou um encerramento inesperado) fazia o download reaparecer como interrompido, e **Continuar fila** o baixava de novo.
- **Download não fica preso em "Analisando".** Se a fila não pudesse ser salva no início de um download, ele continuava aparecendo como ativo sem estar em execução.
- **Antivírus não interrompe mais um download ao salvar o histórico.** Se um antivírus ou o indexador do Windows estivesse lendo o arquivo de histórico no momento em que ele era salvo, o download ou a troca de pasta podia falhar com "Acesso negado". Agora o app aguarda um instante e tenta de novo, como já fazia com a fila.
- **Arquivar histórico não trava o aplicativo.** Com disco lento ou antivírus, o arquivamento podia congelar a janela, interromper o progresso do download ativo e fazer a extensão abrir uma segunda cópia do Dwnl.
- **A extensão não abre outra cópia do app quando ele está ocupado.** Uma resposta lenta deixava de ser confundida com app fechado.
- **Mensagens de erro em português são mantidas após reiniciar.** Antes, o histórico passava a mostrar textos como "Task ended with ENGINE_ERROR".
- **Avisos resolvidos somem.** Avisos como "Outro processo de download está ativo" ou o de pasta inválida desaparecem quando a situação é resolvida.
- **Uma falha ao adicionar um download não bloqueia a fila inteira.** Por exemplo, ao atingir o limite de armazenamento da fila, **Arquivar histórico** continua disponível.
- **A extensão funciona quando algum componente não está disponível.** No estado "requer atenção", analisar e adicionar downloads continua possível, como na janela do app.
- **Seleções antigas não falham com mensagem em inglês.** Depois de muitas análises na mesma sessão, a mensagem passa a pedir, em português, que o vídeo seja analisado de novo.
- **Abrir arquivos grandes funciona.** A janela espera a verificação do arquivo em vez de desistir após 15 segundos, e um tempo esgotado não é mais informado como "arquivo mudou".
- **Mais privacidade na extensão.** As respostas de adicionar e cancelar downloads não enviam mais ao navegador o endereço de origem nem o caminho do arquivo no computador.
- **Correção de um travamento** com títulos ou mensagens contendo texto inválido.
- **Menos trabalho repetido.** Ao concluir um download, o arquivo final não é lido do disco uma segunda vez. As atualizações periódicas de estado usam menos processamento.

## Validação e limites

Os testes Go (Linux), o typecheck, os 163 testes TypeScript e os testes do modelo de interface passaram. O código exclusivo de Windows (app, ponte e comunicação local) foi compilado e verificado com `go vet`.

No Windows 11 x64, a suíte Go completa (`go test -p 1 ./... -count=1`) passou no commit do pacote: 177 testes e 192 subtestes em 12 pacotes, incluindo app, ponte e comunicação local, com as ferramentas de mídia reais. Só o teste opcional do seletor de pasta nativo foi ignorado. O typecheck, os 211 testes TypeScript e os 9 testes do modelo de interface também passaram no Windows. Essa rodada encontrou a falha de "Acesso negado" descrita acima e dois problemas só da infraestrutura de testes, todos corrigidos, e ganhou um teste da ponte com o app ocupado.

O aplicativo empacotado foi exercitado sem instalação, com estado e pasta isolados: download real de um Shorts (H.264/AAC conferido com ffprobe); cancelamento seguido de encerramento forçado (o item reabriu como cancelado e **Continuar fila** não o baixou); estado "requer atenção" com um componente ausente, ainda analisando links; e resposta de adicionar download sem o endereço de origem. O roteiro manual na janela do app instalado e na extensão do Chrome ainda não foi executado.

Continuam valendo os limites da 0.2.0: a transferência interrompida reinicia do começo; playlists e canais não estão incluídos; instalação em Windows limpo e integração com o Chrome pessoal ainda não foram validadas.

Compilado do commit `84d31ea71ad73de3e6f533f02608e0f64f93fff6`, com árvore de código limpa; os commits seguintes alteram só a configuração de testes e estas notas. O `Dwnl.exe` exercitado acima é o mesmo, com o mesmo SHA256, embutido no instalador desta publicação. Um novo build gera arquivos com hashes diferentes, portanto compare sempre com o `SHA256SUMS.txt` desta release. Nenhuma instalação pessoal foi alterada durante a geração.
