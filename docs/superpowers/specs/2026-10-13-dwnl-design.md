# Dwnl — especificação de desenho

Data: 2026-10-13.

Estado: desenho aprovado; especificação escrita aguardando revisão do usuário.

Aprovação do desenho comunicada pelo coordenador: resposta “Sim.” em 2026-10-13 às 19:29:54 UTC. A aprovação cobre a abordagem abaixo. Esta entrega contém somente documentação; não autoriza executar instaladores, registrar auxiliares, publicar, alterar o Chrome ou iniciar implementação antes da revisão desta especificação.

## 1. Objetivo e limites

Uma extensão Chrome Manifest V3 para uso pessoal no Windows, com interface central para baixar vídeo ou gerar MP3 usando um auxiliar local. O usuário aceita baixar o instalador pela extensão e executá-lo manualmente uma vez. Não precisará instalar Python, runtimes ou ferramentas por conta própria nem executar comandos.

YouTube, Instagram, TikTok e X são as quatro plataformas inicialmente pretendidas. Só entram no escopo vídeos públicos acessíveis sem contas, cookies ou controles de acesso a contornar. Conteúdo público ou sem DRM não implica autorização de download: direitos sobre a mídia e permissão para o método de acesso também precisam ser respeitados. Disponibilidade dos extratores varia por URL, data e plataforma. Não há promessa de suporte universal.

O protótipo não terá backend nosso, conta Dwnl, telemetria, serviço no início do Windows, servidor em localhost ou download automático de canais/playlists. Posts com múltiplos vídeos oferecem escolha de um item, sem baixar todos automaticamente. Fotos, transmissões ao vivo e mídia sem duração finita não fazem parte deste primeiro escopo.

Distribuição pública e Chrome Web Store exigiriam avaliação própria de finalidade, privacidade, licenças e políticas. O desenho pessoal não comprova elegibilidade para publicação.

## 2. Comportamento aprovado

### 2.1 Origem e análise

- Ao abrir a interface, o usuário pode colar uma URL ou acionar **Usar aba atual**. Nenhuma aba é analisada continuamente.
- Capturar a URL da aba atual exige ação explícita; o usuário vê a origem antes de iniciar a análise.
- **Analisar** consulta o auxiliar, que retorna apenas dados obtidos: título, duração, itens, formatos, dimensões, codecs e tamanho, quando conhecidos.
- Dados ausentes aparecem como desconhecidos; estimativas de tamanho são identificadas. Não inferir resolução ou bitrate a partir de um rótulo comercial.
- Se houver múltiplos vídeos, selecionar um. Rejeitar playlists/canais e conteúdos fora do escopo em vez de iniciar um lote.
- A análise pode expirar ou a fonte mudar. Antes de iniciar a transferência, validar novamente a seleção; se deixou de existir, pedir nova análise sem substituir silenciosamente qualidade ou item.

### 2.2 Vídeo

- Mostrar qualidades efetivamente retornadas, incluindo dimensões e formato final. Não oferecer aumento de resolução artificial.
- Padrão: melhor resolução disponível até 1080p. Se só existirem resoluções acima desse limite, mostrar as opções sem seleção automática e exigir escolha explícita.
- Quando houver formatos equivalentes, preferir saída MP4 com H.264/AAC. A resolução escolhida pelo usuário não deve cair silenciosamente só para obter MP4.
- Se MP4 compatível não estiver disponível nessa qualidade, informar o formato disponível antes de iniciar, por exemplo WebM. Não recodificar vídeo silenciosamente.
- Áudio e vídeo separados são reunidos por mux/remux sem recodificação, quando compatíveis com o contêiner escolhido.
- Preservar o áudio disponível da mídia. Vídeo originalmente sem áudio pode ser salvo com aviso; isso não habilita o modo MP3.

### 2.3 MP3

- Buscar a melhor fonte de áudio disponível para o item selecionado; baixar vídeo completo somente quando necessário para obter seu áudio.
- Converter para MP3 com bitrate de saída constante: 192 kbps por padrão; opções 128 e 320 kbps.
- Rotular esses valores como **taxa de saída MP3**. Eles não são a qualidade nativa da plataforma e não recuperam fidelidade ausente.
- Usar FFmpeg com encoder MP3 compatível; o pacote deve verificar a capacidade, além da versão. Reamostragem pode ocorrer quando necessária ao encoder, sem promessa de melhoria.
- Se a fonte não tiver áudio decodificável, desabilitar MP3 com motivo explícito.

### 2.4 Destino e confirmação

- Destino inicial: subpasta Dwnl da pasta Downloads real do usuário Windows, resolvida pelas APIs do sistema.
- O usuário pode escolher outra pasta por diálogo do auxiliar. A interface usa um identificador da pasta aprovada, não um caminho arbitrário vindo da página.
- Nome sugerido: título sanitizado e identificador da mídia, com extensão correspondente ao formato real.
- Não sobrescrever arquivos existentes. Gerar nome alternativo e garantir criação sem substituição mesmo se outro processo criar um arquivo simultaneamente.
- Arquivos parciais não aparecem como resultado concluído. Validar o arquivo, reservar o nome final e finalizar dentro do destino escolhido sem deixar um arquivo parcial apresentado como sucesso.
- Quando o destino estiver em outro volume, copiar para um arquivo intermediário exclusivo no próprio destino e só depois finalizar sem substituição. Registrar sua identidade desde a criação; falha de cópia não altera arquivo anterior nem cria resultado final.
- Só declarar conclusão após verificar existência, tamanho não nulo, contêiner e streams esperados com ffprobe. Para MP3, confirmar codec e configuração de codificação; o bitrate total do arquivo pode variar por metadados.

### 2.5 Tarefas e progresso

- Uma tarefa de download/conversão ativa por vez. Duplo clique ou múltiplas interfaces não iniciam cópias da mesma tarefa.
- O auxiliar impõe exclusão de tarefas por usuário Windows, inclusive se outra conexão tentar iniciar simultaneamente; responder **Ocupado**, sem formar fila.
- Estados: analisando, pronto, baixando, juntando, convertendo, salvando, concluído, cancelando, cancelado, falhou ou interrompido. Uma tarefa usa somente as etapas aplicáveis.
- Mostrar progresso de cada etapa. Percentual de download exige total conhecido; conversão pode usar tempo processado/duração confiável. Não inventar percentual global ou tempo restante.
- Fechar o painel não cancela a tarefa. A conexão pertence ao service worker, e não à interface.
- Ao reabrir o painel, recuperar o estado da mesma sessão do auxiliar.
- Encerrar o Chrome ou perder a conexão nativa interrompe a tarefa. Não prometer continuidade depois de fechar o navegador nem reinício automático na reabertura.
- Falhas do motor, falta de disco, destino sem permissão, rede interrompida, auxiliar incompatível, formato indisponível, ausência de áudio e acesso restrito devem ter mensagens diferenciadas.

## 3. Arquitetura e responsabilidades

| Componente | Responsabilidade | Limite |
|---|---|---|
| Interface da extensão | URL/aba atual, análise, seleção, progresso e cancelamento | Não controla shell, credenciais ou executáveis. |
| Controlador MV3 | Validar mensagens, possuir conexão nativa e manter estado para reabrir a interface | Não processa mídia nem depende de variáveis globais para lembrar tarefas. |
| Auxiliar Dwnl | Protocolo, validação, exclusão de tarefas, destino, processos e finalização | Aceita operações tipadas; não é um executor genérico de comandos. |
| Adaptador do motor | Traduzir análise e seleção para uma versão conhecida de yt-dlp | Não expõe flags livres, plugins ou fontes arbitrárias. |
| yt-dlp e Deno | Extração suportada dentro dos limites autorizados | Suporte upstream não é prova de sucesso por plataforma. |
| FFmpeg/ffprobe | Junção, MP3 e validação da saída | Codecs e capacidades dependem da compilação escolhida. |
| Instalador/desinstalador | Pacote privado, manifesto e registro HKCU | Não instala extensões silenciosamente nem adiciona serviço/autostart. |

O contrato do motor permanece separado do protocolo Chrome para permitir substituir o motor posteriormente. Não serão implementados motores adicionais nesta versão.

Native Messaging troca JSON de controle, metadados e progresso. Arquivos e segmentos permanecem no auxiliar; nunca trafegam pelo protocolo nativo. Mensagens são limitadas a no máximo 512 KiB por item em ambas as direções, abaixo do limite documentado do host para o Chrome. Resultados extensos são paginados e metadados opcionais podem ser omitidos com indicação explícita; não truncar IDs ou campos obrigatórios.

## 4. Contrato de comunicação

### 4.1 Envelope e identidade

- Toda solicitação contém versão de protocolo, requestId único, operação e payload validado.
- Após handshake, contém também hostSessionId, que identifica uma execução do auxiliar.
- Respostas repetem requestId e trazem resultado ou erro tipado. Eventos de tarefa incluem hostSessionId, jobId, sequência monotônica, estado e dados de progresso.
- O controlador descarta eventos de outra sessão/tarefa e eventos duplicados ou anteriores.
- Somente o ID esperado da extensão pode se conectar ao host. Mensagens da interface/content script passam por validação de origem, remetente, esquema e limites no controlador e no auxiliar.
- Nenhuma operação recebe strings de comando, flags CLI, código, caminhos de executáveis ou cookies.

### 4.2 Operações

| Operação | Entrada permitida | Resultado |
|---|---|---|
| handshake | Versão do protocolo/extensão | hostSessionId, versão do host/motor, dependências e capacidades como MP3; disponibilidade ou erro. |
| analyze | URL validada e requestId | analysisId da sessão, itens selecionáveis e opções reais identificadas por IDs opacos; validade da análise. |
| chooseDestination | Ação explícita do usuário | destinationId de uma pasta escolhida por diálogo nativo; cancelar não muda o destino atual. |
| start | analysisId, itemId, modo, opção de vídeo ou bitrate MP3 enumerado, destinationId | jobId e confirmação de aceitação, ou ocupado/análise expirada/seleção inválida. |
| status | jobId/requestId de início da sessão ou solicitação de snapshot | Estado autoritativo, requestId de início e última sequência, ou sessão encerrada/tarefa desconhecida. |
| reconcile | hostSessionId anterior e jobId/requestId de início | Consulta somente leitura ao registro anterior: concluído validado, outro terminal ou interrompido/não confirmado; nunca retoma a tarefa. |
| cancel | jobId e requestId | Estado de cancelamento; resultado terminal somente após encerrar os processos. |
| listFormats | analysisId, itemId e cursor opaco | Página de opções e próximo cursor; cursor inválido ou análise expirada é erro. |

Handshake com protocolo incompatível não inicia tarefas. Falta de encoder MP3 pode tornar esse modo indisponível sem declarar toda a instalação funcional para MP3.

### 4.3 Idempotência e decisões incertas

- Na mesma sessão, repetir start com o mesmo requestId e payload retorna o mesmo jobId; payload diferente com o mesmo ID é rejeitado.
- Cancelar uma tarefa já cancelada retorna seu estado. Cancelar uma tarefa já concluída não remove o arquivo final.
- Se a confirmação de start se perder, consultar status da sessão. Não reenviar start com novo ID automaticamente.
- Uma sessão nova nunca executa um start antigo. Se o resultado da operação anterior não puder ser confirmado, marcar como interrompido e exigir uma nova ação do usuário.
- reconcile é a exceção explícita à consulta restrita à sessão atual: aceita somente identidade previamente registrada pelo próprio Dwnl para o usuário Windows atual. Não retorna eventos de execução antigos nem aceita caminhos informados pelo chamador.
- Erros têm código estável, mensagem apresentável e indicação de possibilidade de nova tentativa. URLs assinadas, credenciais ou stderr completo não aparecem na mensagem.

## 5. Estado, reconexão e interrupção

O auxiliar é a autoridade sobre uma tarefa em execução e sobre finalização física. O controlador mantém cache em chrome.storage.local: versão de esquema, hostSessionId, jobId, origem canônica sem tokens, seleção, destino identificado, estado, último evento e resumo terminal. URLs finais de CDN, credenciais e segmentos não são persistidos na extensão.

O cache serve para reconectar o painel e mostrar o último estado; não é evidência isolada de conclusão. A interface inicialmente apresenta **Verificando tarefa** ao recuperar um estado ativo e solicita snapshot. Nunca mostrar um cache antigo como tarefa ainda executando sem confirmação.

Uma única conexão connectNative pertence ao service worker. A interface conecta-se ao controlador e recebe snapshot/eventos. Fechar a interface não destrói a conexão nativa. Se o worker ou a conexão nativa terminar, a tarefa deve ser encerrada no auxiliar. Uma conexão posterior inicia outra sessão e usa reconcile para consultar o registro anterior: conclusão física validada tem precedência sobre o cache; tarefa sem terminal confirmado fica interrompida. Nenhum trabalho anterior é retomado.

Para a janela de finalização, o auxiliar grava registro local mínimo por tarefa em diretório próprio: identidade da sessão/tarefa/requestId, dono e identidade dos temporários, destino final e estado de finalização. Antes de mover o resultado validado, grava um registro preparado com formato, tamanho e hash do arquivo esperado; depois da finalização, grava o terminal concluído. Na reconciliação, registro terminal e arquivo correspondente comprovam conclusão mesmo se o evento se perdeu. Se houve falha entre a movimentação e o registro terminal, a consulta pode confirmar a finalização somente ao verificar identidade, tamanho, hash e mídia no destino já registrado. Qualquer divergência é não confirmada, sem apagar arquivos ou iniciar processos de download/conversão. Um registro incompleto de sessão encerrada não autoriza retomar trabalho; permite somente informar interrupção e localizar resíduos próprios com segurança.

Destino e preferências ficam em configuração privada do auxiliar, validada ao carregar. O histórico na extensão mantém as últimas 20 tarefas até o usuário limpar; análises expiradas não conservam tokens. Apagar histórico não apaga arquivos finais. Configurações de outros programas ou plugins pessoais de yt-dlp não são herdadas.

## 6. Cancelamento e temporários

- Cada tarefa recebe diretório exclusivo e marcador de propriedade criado pelo Dwnl. Todos os subprocessos da tarefa ficam num Windows Job Object, incluindo yt-dlp, Deno e FFmpeg.
- Usar encerramento da árvore inteira e kill-on-job-close; o handle de controle não deve ser herdado pelos filhos, nem permitir escape da árvore. Desconexão/EOF do Chrome também dispara encerramento.
- Não encerrar processos por nome global: outra instalação de FFmpeg/yt-dlp do usuário deve permanecer intacta.
- Só informar **Cancelado** após confirmar encerramento da árvore. Falha de encerramento é erro, não sucesso de cancelamento.
- Sucesso, cancelamento e erro limpam temporários da própria tarefa quando possível. Arquivo final concluído, arquivos preexistentes e temporários de outros programas não são removidos.
- Antes de qualquer limpeza recursiva, resolver caminho absoluto e validar raiz privada, marcador, identidade da tarefa e ausência de tarefa ativa. Rejeitar junctions/symlinks/reparse points que escapem da raiz ou produzam propriedade incerta.
- O intermediário criado no destino de outro volume é uma exceção estritamente pontual: remover somente aquele arquivo cuja criação exclusiva e identidade constam no registro da tarefa, sem reparse point ou divergência de propriedade. Nunca fazer limpeza recursiva da pasta de destino.
- Resíduos de encerramento abrupto podem ser removidos ao abrir o auxiliar, somente após confirmar que pertencem a sessão encerrada e não estão em uso. Resíduo bloqueado é informado; não solicitar limpeza ampla de Downloads ou Temp.
- Se cancelamento disputar com conclusão já confirmada, preservar o arquivo final e reportar concluído. Se ocorreu antes da finalização, remover somente o arquivo parcial próprio.

## 7. Permissões, rede e privacidade

### 7.1 Extensão

Permissões inicialmente necessárias: nativeMessaging para o auxiliar, storage para preferências/estado e activeTab para obter a URL atual após gesto. downloads só é necessária no pacote do protótipo que realmente oferece o instalador incluído. Não solicitar tabs, scripting, cookies, history, debugger, acesso a arquivos ou hosts amplos preventivamente.

Capturar apenas a URL da aba atual não exige inspeção do DOM. Se uma necessidade futura mudar esse limite, rever a permissão e a especificação. Não criar content scripts permanentes, externally_connectable ou recursos do auxiliar acessíveis às páginas.

O JavaScript da extensão permanece empacotado. Atualizações do motor não serão entregues como código remoto executado pela extensão.

### 7.2 URLs e argumentos

- Aceitar somente HTTPS, sem usuário/senha na URL, e hosts de entrada explicitamente autorizados por adaptador: domínios oficiais e formas curtas documentadas das quatro plataformas. Não aceitar substrings como prova de domínio.
- Rejeitar file, data, javascript, localhost, endereços privados/link-local e entradas ambíguas. Canonicalização deve preservar parâmetros necessários ao identificador da mídia, remover rastreamento irrelevante e não armazenar tokens.
- Redirecionamentos e URLs de mídia obtidas pelo extrator também precisam de validação. Os domínios CDN permitidos serão documentados por adaptador; validar destino resolvido/conectado e bloquear destinos internos. Um hostname público sozinho não resolve DNS rebinding.
- A integração precisa demonstrar que essas restrições valem para as requisições do motor, não só para a URL inicial. Se o motor escolhido não permitir garantir uma restrição necessária, essa capacidade fica bloqueada até resolver a integração.
- Invocar executáveis de caminhos privados fixos, sem shell e sem concatenação de comandos. IDs/opções devem pertencer à análise e à sessão atual.
- Isolar configurações/plugins herdados; não permitir execução pós-download arbitrária, credenciais de navegador, netrc, proxies para contornar bloqueios, DRM, paywalls ou desafios de autenticação/anti-bot.
- O pacote EJS é uma dependência técnica, não autorização para contornar controles ou aceitar condições de uso indevidas.

### 7.3 Caminhos e dados

Validar destinos reais e permissões de escrita; aceitar mudanças de pasta somente por ação do usuário. Sanitizar nomes, extensões, nomes reservados Windows, separadores, caminhos absolutos e tentativas de traversal. Não confiar em títulos, metadados ou caminhos retornados por terceiros.

URLs selecionadas são enviadas do computador às plataformas/CDNs para a tarefa. Não passam por backend nosso. Logs locais são limitados e não incluem credenciais, tokens de mídia ou páginas completas. Diagnóstico compartilhado depende de ação explícita do usuário e deve ser revisável.

Embora local, esse tratamento de URLs e histórico deve ser explicado na interface e documentação. Não alegar ausência total de tratamento de dados.

## 8. Pacote Windows e ciclo de instalação

### 8.1 Conteúdo e versões

O pacote contém auxiliar Dwnl autocontido, yt-dlp.exe oficial, deno.exe, ffmpeg.exe e ffprobe.exe, com eventuais DLLs exigidas pela compilação FFmpeg escolhida. Python e yt-dlp-ejs já estão incluídos no executável oficial yt-dlp. Deno é binário privado; não exigir runtime global nem mudar PATH.

Cada entrega registra versão exata, arquitetura, URL oficial de origem, hash, assinatura disponível, capacidades e licenças de cada dependência. Nenhuma versão numérica foi escolhida nesta especificação e nenhum binário foi obtido. A escolha concreta depende das versões compatíveis existentes quando o pacote for preparado; não é um valor “latest” flutuante.

A documentação consultada indica Deno 2.3.0 como mínimo aceito pelo yt-dlp atual. Usar combinação compatível e validada, não supor que qualquer Deno/FFmpeg serve. Handshake verifica protocolo, presença de ferramentas, ffprobe, encoder MP3 e capacidades relevantes.

Atualizações são entregas verificadas do conjunto, acionadas pelo usuário. Não baixar componentes EJS remotos ou atualizar executáveis silenciosamente durante uma tarefa. Mudança de motor/capacidades exige repetir validação das plataformas afetadas.

### 8.2 Licenças

| Dependência | Situação documentada | Exigência para o pacote |
|---|---|---|
| yt-dlp | Código-base Unlicense; o trabalho combinado dos executáveis PyInstaller oficiais é licenciado sob GPLv3+ | Conservar avisos/licenças do executável escolhido e cumprir os termos de redistribuição, inclusive fonte correspondente quando aplicável. |
| yt-dlp-ejs | Unlicense com componentes MIT/ISC | Preservar avisos correspondentes à versão embutida. |
| Deno | MIT, com dependências de terceiros | Incluir licença, copyright e avisos de terceiros do binário escolhido. |
| FFmpeg/ffprobe | LGPL-2.1+ ou GPL conforme a compilação | Identificar flags e dependências; não escolher build nonfree para redistribuir; fornecer licenças e fonte correspondente conforme os termos aplicáveis. |

Não concluir licença do FFmpeg a partir da licença de um repositório de scripts de build. A extensão e o auxiliar usam código/interface próprios; não incorporam Cobalt/DLPanda ou seus ativos.

### 8.3 Identidade e registro

- Manter ID estável da extensão descompactada por chave pública local no campo key do manifesto, consistente com a origem no instalador. Não usar upload para a Web Store para obter esse ID neste protótipo.
- A chave privada de empacotamento, se necessária ao processo posterior, fica fora do repositório. A chave pública não é segredo. Nenhuma chave foi criada nesta etapa.
- O instalador por usuário escreve o manifesto do host na pasta privada Dwnl e registra somente HKCU\Software\Google\Chrome\NativeMessagingHosts\<nome-do-host>.
- allowed_origins contém somente o ID exato esperado, sem curingas. Verificar origem na conexão; não cadastrar outras extensões automaticamente.
- Chrome inicia o host sob demanda por stdio. Não instalar serviço, tarefa agendada, porta de rede, entrada de startup ou extensão silenciosa.
- Desinstalar remove apenas o registro apontando para a instalação Dwnl verificada e os arquivos próprios. Se o registro tiver sido alterado para outra instalação, não apagar cegamente.
- Preservar downloads finais e a extensão carregada pelo usuário; informar como removê-la separadamente. Antes de remover arquivos em uso, exigir encerramento das tarefas/conexões. Oferecer remoção de preferências/histórico locais explicitamente.

### 8.4 Bootstrap sem hospedagem pública

Ainda não existe instalador ou URL de distribuição. O protótipo será entregue futuramente como pacote local: extensão descompactada, instruções e instalador produzido para essa mesma identidade/versão. Não publicar servidor, liberar uma API de terceiros ou inventar URL de release.

O pacote da extensão do protótipo pode incluir o instalador como recurso privado. Depois de o usuário carregar explicitamente a extensão descompactada, o botão **Baixar instalador do Dwnl** resolve o recurso realmente presente via runtime.getURL e solicita sua cópia com downloads. Esse recurso não entra em web_accessible_resources. O uso do recurso local por downloads é uma hipótese de integração a validar no Chrome, não comportamento já comprovado.

Se essa transferência não funcionar no ambiente suportado, o onboarding identifica o instalador fornecido no pacote local e orienta sua abertura manual. Não exibe botão para um recurso ausente nem inventa uma URL; não exige servidor localhost ou permissão de acesso file. Essa alternativa preserva a abertura manual única já aceita.

O usuário precisa executar o instalador. A extensão não promete instalação/execução silenciosa. Após instalação, nova tentativa de conexão realiza handshake e mostra disponibilidade dos modos. Detecção ausente, erro de protocolo e dependência incompleta têm estados distintos.

O carregamento manual da extensão no Chrome é uma etapa de instalação do protótipo, separada do registro do auxiliar. A abordagem aprovada não autoriza executar essas ações agora.

## 9. Critérios de aceitação e evidência

Todos os critérios abaixo são requisitos futuros. Nenhum teste de produto, instalação ou download foi executado nesta etapa documental.

| ID | Critério observável | Evidência necessária |
|---|---|---|
| A01 | URL colada e aba atual por gesto produzem a mesma origem para análise; outras abas não são lidas. | Integração real Chrome, inspeção de permissões e rede. |
| A02 | Qualidades mostradas correspondem aos formatos retornados; default respeita até 1080p e não reduz qualidade silenciosamente para MP4. | Testes com respostas controladas e conteúdo real autorizado. |
| A03 | Sem áudio desabilita MP3; saídas 128/192/320 são MP3 reproduzíveis com encoder/configuração esperados. | Fixtures locais conhecidas, ffprobe e reprodução; não só mocks. |
| A04 | Junção preserva streams, resolução e sincronização, sem recodificação de vídeo. | Fixture A/V conhecida, inspeção de codecs, comparação audiovisual e invocação efetiva do FFmpeg vinculada à saída, comprovando streamcopy sem filtros/encoder de vídeo. |
| A05 | Duplo clique, pedido duplicado e segunda conexão não iniciam outra tarefa; conflito retorna ocupado. | Teste de integração do protocolo e processos reais. |
| A06 | Fechar/reabrir interface recupera a tarefa da sessão viva; terminar worker interrompe, e cache antigo não aparece como execução confirmada. | Chrome real: reabrir interface sem encerrar worker e, separadamente, terminar/reiniciar worker e verificar interrupção ou terminal reconciliado. |
| A07 | Encerrar Chrome/conexão/host elimina toda a árvore e não reinicia tarefa ao reabrir. | Windows real, observar PIDs/Job Object e estado reconciliado. |
| A08 | Cancelar durante download e MP3 encerra todos os filhos; outro FFmpeg do usuário permanece. | Processos reais e processo independente de controle. |
| A09 | Limpeza não sai da raiz própria, não segue junctions externas nem apaga destino/final de outra tarefa. | Testes de filesystem isolado, marcadores e resíduos de falha. |
| A10 | Disco cheio/permissão negada/arquivo conflitante falham sem falso sucesso, perda de arquivo anterior ou arquivo parcial final. | Falhas induzidas e leitura dos arquivos após o teste. |
| A11 | Esquemas, URLs, argumentos, origens, nomes e redirecionamentos indevidos são rejeitados; sem shell/cookies/plugins herdados. | Testes negativos e inspeção da invocação/rede real do motor. |
| A12 | Instalação funciona sem Python/Deno/FFmpeg globais e sem comandos do usuário; protocolo e ID correspondem. | Windows limpo ou perfil de teste autorizado com pacote real. |
| A13 | Bootstrap transfere o instalador incluído ou oferece alternativa local honesta; não executa silenciosamente. | Chrome real; registrar qual caminho foi validado. |
| A14 | Desinstalação remove somente artefatos/registro próprios, conserva downloads e não afeta outro host. | Readback de filesystem/registro em ambiente de teste autorizado. |
| A15 | Cada plataforma passa por análise, vídeo, MP3 quando houver áudio, validação do arquivo e reprodução. | Casos reais autorizados, com data, URL/identificador, formatos, versões e resultado. |
| A16 | Login, conteúdo privado, DRM e bloqueio que exija contorno são reportados sem solicitar credenciais ou tentar evasão. | Casos negativos controlados e casos reais permitidos para inspeção. |
| A17 | Progresso indeterminado e fases de conversão não geram percentuais/tempo restante falsos. | Respostas/eventos controlados e observação de tarefas reais. |
| A18 | Evento final perdido é reconciliado por consulta somente leitura ao registro anterior e arquivo validado; operação incerta não é reiniciada nem duplica saída. | Interrupção induzida antes/depois da movimentação final e do registro terminal; testar divergência de hash e destino em outro volume. |

Em fixtures com duração conhecida, saída deve diferir no máximo por max(0,5 segundo, 1% da duração); a fixture audiovisual deve permitir observar sincronização com diferença de até 100 ms. Qualquer exceção de contêiner/encoder deve ser registrada e revisada, não tratada como teste aprovado silenciosamente.

### 9.1 Matriz por plataforma

| Plataforma | Evidência atual | Condição para declarar validada |
|---|---|---|
| YouTube | Pretendida; dependências e restrições documentadas; nenhuma URL testada. | Caso real público com método autorizado, vídeo e MP3 quando disponível; registrar limitações/impedimentos. |
| Instagram | Pretendida; nenhuma URL testada. | Caso real público sem login, análise e arquivo final validados. |
| TikTok | Pretendida; nenhuma URL testada. | Caso real público sem login, análise e arquivo final validados; forma curta só se redirecionamento seguro for validado. |
| X | Pretendida; nenhuma URL testada. | Caso real público sem login, análise e arquivo final validados; múltiplos vídeos exigem seleção correta. |

Mocks comprovam comportamento da interface/contrato diante de uma resposta; fixtures locais comprovam processamento. Nenhum dos dois comprova funcionamento de um extrator numa plataforma. Estar na lista upstream também não comprova funcionamento.

Uma validação real exige autorização adequada para conteúdo e método, conta livre de credenciais no fluxo, versões registradas e conferência audiovisual do resultado. Sem caso autorizado/viável, marcar **Não validada**, **Restrita** ou **Indisponível**, com motivo. Não declarar “compatível com todos os vídeos” depois de um caso positivo.

A evidência deve ser repetida quando mudança de extrator/dependência ou falha observada afetar a plataforma. Testes reais futuros dependem de autorização para executar o pacote no ambiente escolhido; esta especificação não os executa.

## 10. Lacunas e decisões ainda não técnicas de execução

- As quatro plataformas ainda não têm casos reais autorizados selecionados ou suporte comprovado. YouTube exige atenção adicional aos termos, independentes de visibilidade e DRM.
- Versões/builds/arquitetura exatos e linguagem do auxiliar serão escolhidos na implementação, com os critérios de pacote autocontido e capacidades acima. Confirmar Windows/arquitetura antes de produzir o instalador; Deno atual requer Windows 10 1709 ou posterior.
- Validar a transferência do instalador como recurso privado de extensão no Chrome. A alternativa local já está definida e não exige hospedagem.
- Validar enforcement de rede do motor, lifecycle/Job Object, registro por usuário e finalização em destinos de volumes diferentes.
- Assinatura de código, tamanho do pacote e possíveis avisos de Windows/Chrome só poderão ser avaliados com o artefato escolhido. Não prometer instalador sem avisos.

Nenhuma dessas lacunas impede a revisão deste desenho. Falha de validação futura pode limitar uma plataforma ou exigir revisão da integração, sem autorização automática para contornar bloqueios. Esta especificação não contém plano de implementação.

## 11. Fontes primárias consultadas

Documentação consultada em 2026-10-13; referências externas podem mudar.

- [Chrome: Native Messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging)
- [Chrome: ciclo de vida MV3](https://developer.chrome.com/docs/extensions/develop/concepts/service-workers/lifecycle)
- [Chrome: Downloads API](https://developer.chrome.com/docs/extensions/reference/api/downloads)
- [Chrome: activeTab](https://developer.chrome.com/docs/extensions/develop/concepts/activeTab)
- [Chrome: identidade pelo campo key](https://developer.chrome.com/docs/extensions/reference/manifest/key)
- [Chrome Web Store: produtos proibidos](https://developer.chrome.com/docs/webstore/program-policies/malicious-and-prohibited)
- [Chrome Web Store: tratamento local de dados](https://developer.chrome.com/docs/webstore/program-policies/user-data-faq)
- [YouTube: termos](https://www.youtube.com/static?template=terms)
- [YouTube: políticas de APIs](https://developers.google.com/youtube/terms/developer-policies)
- [yt-dlp: dependências, processamento e licenças](https://github.com/yt-dlp/yt-dlp)
- [yt-dlp: EJS e runtime](https://github.com/yt-dlp/yt-dlp/wiki/EJS)
- [Deno: distribuição autocontida](https://docs.deno.com/runtime/getting_started/installation/)
- [Deno: licença](https://github.com/denoland/deno/blob/main/LICENSE.md)
- [FFmpeg: streamcopy](https://ffmpeg.org/ffmpeg.html#Streamcopy)
- [FFmpeg: encoder MP3](https://ffmpeg.org/ffmpeg-codecs.html#libmp3lame)
- [FFmpeg: licenças](https://ffmpeg.org/legal.html)
- [Microsoft: Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)
