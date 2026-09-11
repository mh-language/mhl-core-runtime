/* Complete files: each lesson is checked with the repository's mhl lint. */
(function (root) {
  const lessons = [];
  function lesson(id, group, title, description, concepts, code, result, notes, challenge, source, options = {}) {
    lessons.push({ id, group, title, description, concepts: concepts.split(" · "),
      file: id + ".mh", code: code.join("\n"), result, notes, challenge, source,
      mode: "run", args: "", requirements: "Somente o runtime MHL.", ...options });
  }

  lesson("valores", "Fundamentos", "Valores e variáveis",
    "Crie um programa completo. As instruções executáveis ficam dentro de um step; var pode mudar e const não pode ser reatribuída.",
    "pipeline · step · var · const · interpolação", [
      'pipeline Hello {',
      '    step Greet {',
      '        const LIMIT = 3',
      '        var attempt = 1',
      '        var project = "mhl"',
      '        attempt += 1',
      '        log("${project}: tentativa ${attempt} de ${LIMIT}")',
      '    }',
      '}'
    ], "mhl: tentativa 2 de 3",
    ["A interpolação ${expressão} inclui valores em strings.", "Uma variável declarada dentro do step pertence àquele passo."],
    "Troque o nome do projeto e incremente attempt mais uma vez. Qual será a mensagem?",
    "sample/syntax/const_declaration/const_holds_a_single_value.mh");

  lesson("colecoes", "Fundamentos", "Coleções e dados opcionais",
    "Filtre objetos, transforme arrays com lambdas e forneça valores padrão para campos ausentes.",
    "array · object · filter · map · ?. · ??", [
      'pipeline SelectTasks {',
      '    step Select {',
      '        var tasks = [',
      '            {name: "lint", priority: 2},',
      '            {name: "test", priority: 1},',
      '            {name: "deploy", priority: 3}',
      '        ]',
      '        var urgent = tasks.filter((task) -> task.priority > 1)',
      '        var names = urgent.map((task) -> task.name)',
      '        var config = {}',
      '        var owner = config?.owner ?? "team"',
      '        var selected = names.join(", ")',
      '        log("${owner}: ${selected}")',
      '    }',
      '}'
    ], "team: lint, deploy",
    ["map e filter retornam novos arrays.", "?. permite acesso opcional; ?? usa o valor à direita quando o valor à esquerda é null."],
    "Filtre apenas priority > 2 e adicione owner ao objeto config.",
    "sample/syntax/higher_order_array/map.mh");

  lesson("decisoes", "Fundamentos", "Tipos, enum e match",
    "Nomeie tipos e estados possíveis. Combine uma decisão booleana com uma seleção exaustiva.",
    "type · enum · if / else · match", [
      'type Score = number',
      'enum Status { Pending, Passed, Failed }',
      '',
      'pipeline Decide {',
      '    input score: Score',
      '    step Classify {',
      '        var status = if (score >= 7) Status.Passed else Status.Failed',
      '        var message = match status {',
      '            Status.Pending -> "aguardando"',
      '            Status.Passed -> "aprovado"',
      '            Status.Failed -> "reprovado"',
      '        }',
      '        log(message)',
      '    }',
      '}'
    ], "Com --input score=8:\naprovado",
    ["type cria um alias, não um tipo distinto.", "mhl lint verifica a cobertura das variantes de enum no match."],
    "Execute com score=5. Depois remova uma variante do match e rode mhl lint.",
    "sample/syntax/enum_and_match/match_over_an_enum.mh", { args: "--input score=8" });

  lesson("lacos", "Fundamentos", "for e while",
    "Percorra uma coleção ou repita uma operação com uma condição explícita de parada.",
    "for · while · if · condição de parada", [
      'pipeline Count {',
      '    step Sum {',
      '        var total = 0',
      '        for (var n in [1, 2, 3, 4]) {',
      '            if (n != 2) total += n',
      '        }',
      '        var attempt = 0',
      '        while (attempt < 2) {',
      '            attempt += 1',
      '        }',
      '        log("total=${total}; tentativas=${attempt}")',
      '    }',
      '}'
    ], "total=8; tentativas=2",
    ["O while repete instruções dentro do passo. loop pipeline repete a sequência de passos.", "MHL não possui continue. break encerra a execução do pipeline, não apenas o for ou while mais próximo."],
    "Remova a condição n != 2 e preveja o novo total antes de executar.",
    "sample/syntax/looping_constructs/for_in_loop.mh");

  lesson("ferramentas-testes", "Fundamentos", "Ferramentas e testes",
    "Extraia uma operação tipada e registre exemplos executáveis com as asserções da linguagem.",
    "tool · parâmetros padrão · test · describe · are_equal", [
      'tool Text {',
      '    slug(value: string, separator: string = "_"): string ->',
      '        value.trim().to_lower().replace(" ", separator)',
      '}',
      '',
      'test TextExamples {',
      '    describe default_separator {',
      '        are_equal(Text.slug(" Hello MHL "), "hello_mhl")',
      '    }',
      '    describe custom_separator {',
      '        are_equal(Text.slug("Hello MHL", "-"), "hello-mhl")',
      '    }',
      '}'
    ], "2 casos devem passar com mhl test.",
    ["test e describe isolam exemplos verificáveis.", "Imports permitem compartilhar declarações: import {Text} from \"text.mh\" (exporte Text nesse módulo)."],
    "Adicione um describe que verifique uma palavra sem espaços.",
    "sample/syntax/param_defaults/omitted_argument_uses_the_default.mh", { mode: "test" });

  lesson("pipeline", "Orquestração", "Entradas, passos e saída",
    "Compartilhe estado entre passos e exponha um contrato de resultado explícito.",
    "input · var · step · output", [
      'pipeline ReviewChange {',
      '    input diff: string',
      '    var risk = "unknown"',
      '',
      '    step Inspect {',
      '        risk = if (diff.contains("password")) "high" else "low"',
      '    }',
      '    step Decide {',
      '        log(if (risk == "high") "manual review" else "approved")',
      '    }',
      '    output: {risk: risk}',
      '}'
    ], 'Com --input diff=password:\nmanual review\nContrato de saída: {risk: "high"}',
    ["Declare var no corpo do pipeline para compartilhar o valor entre passos.", "output projeta apenas os campos que o chamador deve receber."],
    "Adicione uma variável interna e confira que ela não aparece no contrato output.",
    "sample/syntax/pipeline_vs_workflow/linear_pipeline_and_branching_workflow.mh", { args: "--input diff=password" });

  lesson("agentes", "Orquestração", "Prompts e agentes",
    "Separe a instrução parametrizada da configuração de chamada do agente. Echo permite testar a ligação sem uma API de IA.",
    "agent · prompt · run · argumentos nomeados", [
      'agent Echo {',
      '    command: "echo"',
      '}',
      'prompt Review(file: string, focus: string) {',
      '    "Review ${file}. Focus on ${focus}."',
      '}',
      '',
      'pipeline AgentReview {',
      '    step ReviewFile {',
      '        var result = Echo.run(prompt: Review(file: "api.go", focus: "security"))',
      '        log(result)',
      '    }',
      '}'
    ], "Review api.go. Focus on security.",
    ["Echo devolve o prompt; a configuração command/args pode apontar para uma CLI de modelo.", "Uma chamada de agente acontece no runtime local. O navegador não inicia processos."],
    'Passe focus: "performance" para Review. Os argumentos desse prompt são nomeados.',
    "sample/features/prompts/prompt_renders_declared_template.mh", { requirements: "Runtime MHL e o executável echo no PATH." });

  lesson("roteador", "Orquestração", "Router e delegação entre agentes",
    "Declare um router sobre agentes já declarados e decida qual deles atende um prompt: primeiro por uma regra determinística, depois, se necessário, por uma chamada de decisão.",
    "router · select · delegate · decider · nameof", [
      'agent Billing { command: "echo" args: ["billing-said:"] }',
      'agent Support { command: "echo" args: ["support-said:"] }',
      'agent Judge { command: "echo" args: ["support-said:"] }',
      '',
      'router Frontdesk {',
      '    agents: [Billing, Support]',
      '    select: (prompt) -> {',
      '        if (prompt.contains("invoice")) return nameof(Billing)',
      '        return null',
      '    }',
      '    decider: Judge',
      '}',
      '',
      'pipeline Helpdesk {',
      '    step Handle {',
      '        var reply = Frontdesk.delegate(prompt: "my invoice is wrong")',
      '        log(reply)',
      '    }',
      '}'
    ], 'billing-said: my invoice is wrong\n\nSem "invoice" no prompt, select retorna null e Frontdesk decide pela chamada de decisão através do agent Judge (aqui, echo simula essa decisão).',
    ["select roda primeiro; um retorno que bate com um nome em agents evita a chamada de decisão.", "Sem select, ou quando ele não decide, delegate roda o decider (um agent comum, declarado à parte) com uma chamada de decisão e então roda o agente escolhido com o mesmo prompt.", "decider é opcional: omita-o por completo para um router puramente determinístico — nesse caso select precisa cobrir todo prompt sozinho.", "nameof(Billing), em vez da string \"Billing\", faz o typo virar erro do mhl lint e deixa \"ir para definição\" do editor funcionar; um retorno que não bate com nenhum agent declarado sempre falha na hora, nunca é tratado como select indeciso."],
    'Troque o prompt para não mencionar "invoice" e preveja qual agente responde.',
    "sample/features/router/router_select_hook_resolves_deterministically.mh", { requirements: "Runtime MHL e o executável echo no PATH." });

  lesson("loop-max", "Orquestração", "Limite de execuções",
    "Repita o pipeline inteiro com um teto explícito de iterações. mem mantém o valor entre as repetições.",
    "loop pipeline · max · mem · break", [
      'loop pipeline Refine max 3 {',
      '    mem rounds = 0',
      '',
      '    step Improve {',
      '        rounds += 1',
      '        log("rodada ${rounds}")',
      '        // if (rounds == 2) break "suficiente"',
      '    }',
      '}'
    ], "rodada 1\nrodada 2\nrodada 3",
    ["max 3 equivale a repeat: { max_iterations: 3 }. Use uma das formas.", "Esse limite conta iterações completas. break encerra antecipadamente a execução, pulando os passos restantes e novas iterações."],
    "Mude max 3 para max 2 e acrescente um segundo step. Quantos passos executarão?",
    "sample/syntax/pipeline_vs_workflow/linear_pipeline_and_branching_workflow.mh");

  lesson("loop-condicao", "Orquestração", "Loop até uma condição",
    "Use uma condição observável junto com um teto de segurança, em vez de depender apenas de um número fixo de repetições.",
    "repeat · stop_when · max_iterations · mem", [
      'loop pipeline Poll {',
      '    mem checks = 0',
      '    mem status = "pending"',
      '    repeat: {',
      '        stop_when: status == "done"',
      '        max_iterations: 5',
      '    }',
      '',
      '    step Check {',
      '        checks += 1',
      '        status = if (checks >= 3) "done" else "pending"',
      '        log("check ${checks}: ${status}")',
      '    }',
      '}'
    ], "check 1: pending\ncheck 2: pending\ncheck 3: done",
    ["stop_when pode ler mem ou um store memory. Variáveis locais do passo não estão no seu escopo.", "O runtime encerra o loop quando a condição é satisfeita ou o teto é atingido."],
    "Troque checks >= 3 por checks >= 8. O teto de 5 impede uma sexta iteração.",
    "sample/features/pipelines/loop_poll_pipeline_example.mh");

  lesson("timeout", "Concorrência e limites", "Timeout de passo",
    "Limite a duração das chamadas de um passo. time.sleep torna o efeito do prazo fácil de reproduzir.",
    "step timeout · time.sleep · duração · --resume", [
      'pipeline TimedWork {',
      '    step Prepare timeout 2s {',
      '        time.sleep(20ms)',
      '        log("ready")',
      '    }',
      '    step Finish {',
      '        log("finished")',
      '    }',
      '}'
    ], "ready\nfinished\n\nExperimento: com time.sleep(3s), Prepare excede 2s e Finish não executa.",
    ["O timeout do passo é declarado sem dois-pontos: step Prepare timeout 2s.", "O prazo cancela chamadas bloqueantes. Na retomada, o passo recebe um novo prazo; isso não aumenta max_iterations."],
    "Mude o sleep para 3s. Execute na CLI para observar a falha por timeout.",
    "sample/features/time/time_sleep_pauses_execution.mh");

  lesson("spawn-wait", "Concorrência e limites", "spawn, wait e concorrência",
    "Dispare chamadas concorrentes dentro de um passo e aguarde os resultados com um prazo explícito.",
    "spawn · wait · timeout: · max_concurrency · task", [
      'agent Echo { command: "echo" }',
      '',
      'pipeline ConcurrentReview {',
      '    spawn: { max_concurrency: 2 }',
      '',
      '    step Analyze timeout 5s {',
      '        spawn docs = Echo.run(prompt: "docs ready")',
      '        spawn tests = Echo.run(prompt: "tests ready")',
      '        spawn security = Echo.run(prompt: "security ready")',
      '        wait docs, tests, security timeout: 2s',
      '',
      '        log(docs.result)',
      '        log(tests.result)',
      '        log(security.result)',
      '    }',
      '}'
    ], "docs ready\ntests ready\nsecurity ready\n\nNo máximo 2 chamadas de agentes em voo; wait aguarda as 3.",
    ["max_concurrency limita as chamadas em voo na execução. wait timeout: limita a espera; step timeout limita o passo inteiro.", "Handles expõem .result, .status, .ok e .error. Chamadas não aguardadas são reunidas ao terminar o passo."],
    "Mude max_concurrency para 1. As três tarefas continuam existindo, mas as chamadas ficam serializadas.",
    "sample/features/pipelines/concurrent_agents_pipeline_example.mh", { requirements: "Runtime MHL e echo no PATH." });

  lesson("fan-out", "Concorrência e limites", "Fan-out e políticas de espera",
    "Inicie uma chamada por item e escolha entre esperar todas, o primeiro sucesso ou um quórum.",
    "spawn for · wait any · wait N of · on_error", [
      'agent Echo { command: "echo" }',
      '',
      'pipeline CheckFiles {',
      '    spawn: { max_concurrency: 2 }',
      '    step Check timeout 5s {',
      '        var files = ["api.go", "auth.go", "main.go"]',
      '        spawn checks = Echo.run(prompt: "check ${item}") for item in files',
      '        wait checks timeout: 2s on_error: "collect"',
      '        for (var task in checks) {',
      '            log("${task.status}: ${task.result}")',
      '        }',
      '        // Alternativas ao wait acima:',
      '        // wait any checks timeout: 2s',
      '        // wait 2 of checks timeout: 2s',
      '    }',
      '}'
    ], "3 handles, um por arquivo.\nwait checks: aguarda todos.\nwait any: primeiro sucesso.\nwait 2 of: dois sucessos.\non_error: \"collect\": examine .ok e .error de cada handle.",
    ["any e quórum encerram a espera ao obter sucessos suficientes e cancelam tarefas restantes.", "collect permite inspecionar falhas individuais. Não garante que todos os agentes tenham sucesso."],
    "Substitua o wait ativo por wait any. Trate .ok antes de usar .result das tarefas canceladas.",
    "sample/features/pipelines/concurrent_agents_pipeline_example.mh", { requirements: "Runtime MHL e echo no PATH." });

  lesson("parallel", "Concorrência e limites", "Passos paralelos",
    "Execute passos inteiros em paralelo, com prazos próprios e um prazo para a barreira do grupo.",
    "parallel · timeout · step · barreira", [
      'pipeline Enrichment {',
      '    var docs = ""',
      '    var issues = ""',
      '',
      '    parallel Gather timeout 2s {',
      '        step Docs timeout 1s {',
      '            time.sleep(20ms)',
      '            docs = "docs ready"',
      '        }',
      '        step Issues {',
      '            time.sleep(30ms)',
      '            issues = "issues ready"',
      '        }',
      '    }',
      '    step Merge {',
      '        log("${docs}; ${issues}")',
      '    }',
      '}'
    ], "Gather → Docs + Issues → barreira → Merge\ndocs ready; issues ready",
    ["O prazo que vencer primeiro prevalece: grupo ou passo.", "As escritas são mescladas na barreira; atribuições conflitantes à mesma variável falham. Uma retomada refaz o grupo."],
    "Coloque time.sleep(3s) em Issues e observe que o grupo excede o prazo antes de Merge.",
    "sample/features/pipelines/parallel_steps_pipeline_example.mh");

  lesson("memoria", "Resiliência e integrações", "Memória e sessão",
    "Persista dados explicitamente e use o identificador de sessão para separar execuções.",
    "memory · json · context · set · get", [
      'memory Session {',
      '    type: "json"',
      '    path: ".mhl/session.${context.session_id}.json"',
      '}',
      '',
      'pipeline Remember {',
      '    step Save {',
      '        Session.set("status", "reviewed")',
      '        Session.set("started_at", context.started_at)',
      '    }',
      '    step Read {',
      '        log(Session.get("status"))',
      '    }',
      '}'
    ], "reviewed\n\nA CLI grava .mhl/session.<session-id>.json.",
    ["memory é um store explícito; mem é estado que atravessa iterações de um loop.", "context.session_id e context.started_at estão disponíveis sem declarar context:."],
    "Salve e leia um objeto com dois campos na mesma sessão.",
    "sample/features/pipelines/session_scoped_memory_pipeline_example.mh", { requirements: "Runtime MHL; a execução cria um arquivo JSON em .mhl/." });

  lesson("retomada", "Resiliência e integrações", "Workflow e aprovação humana",
    "Ramifique com goto e suspenda a execução até uma decisão externa. O checkpoint permite retomar o passo.",
    "workflow · goto · pause · checkpoint · --resume", [
      'workflow Approval {',
      '    input approved: bool',
      '    checkpoint: { ttl: 7d }',
      '',
      '    step Gate {',
      '        if (!approved) pause("Aguardando aprovação")',
      '        goto Publish',
      '    }',
      '    step Publish {',
      '        log("approved")',
      '    }',
      '}'
    ], "Com approved=false: execução suspensa em Gate.\nRetome com approved=true: Gate é reexecutado e Publish imprime approved.",
    ["goto é permitido em workflow, não em pipeline.", "pause é um sinal de suspensão, não uma exceção capturada por try/catch. Evite efeitos não idempotentes antes da pausa."],
    "Execute com approved=false; depois use o comando de retomada com approved=true.",
    "sample/syntax/pipeline_vs_workflow/linear_pipeline_and_branching_workflow.mh",
    { args: "--input approved=false", resume: "mhl run retomada.mh --resume --input approved=true" });

  lesson("falhas", "Resiliência e integrações", "Tentativas e tratamento de falhas",
    "Defina o teto de tentativas do agente e trate o erro quando a chamada não produzir um resultado.",
    "retry · max_attempts · delay · try / catch · fail", [
      'agent Echo {',
      '    command: "echo"',
      '    retry: {',
      '        max_attempts: 3',
      '        delay: 100ms',
      '        backoff: "exponential"',
      '    }',
      '}',
      '',
      'pipeline SafeCall {',
      '    step Call timeout 3s {',
      '        try {',
      '            var reply = Echo.run(prompt: "ok")',
      '            log(reply)',
      '        } catch (error) {',
      '            log("agent failed")',
      '            fail("revisão indisponível")',
      '        }',
      '    }',
      '}'
    ], "ok\n\nUma chamada bem-sucedida não consome as tentativas restantes.",
    ["max_attempts inclui a primeira tentativa. O prazo do passo também limita a chamada e suas tentativas.", "cache: { strategy: \"exact\", ttl: 1h } permite reutilizar respostas; avalie se o prompt e os efeitos tornam isso adequado."],
    "No seu ambiente, use um agente que falhe para observar retry e catch.",
    "sample/features/agents/fixtures/agents.mh", { requirements: "Runtime MHL e echo no PATH." });

  lesson("integracoes", "Resiliência e integrações", "MCP, A2A e serviços externos",
    "Conecte ferramentas MCP e agentes A2A com configuração declarativa e credenciais do ambiente.",
    "extension mcp · extension a2a · env · call · send", [
      'extension mcp Issues {',
      '    transport: "http"',
      '    url: "https://mcp.example.com"',
      '    headers: { "Authorization": "Bearer " + env("MCP_TOKEN") }',
      '}',
      'extension a2a Translator {',
      '    url: "https://translator.example.com/a2a"',
      '    poll_interval: 1s',
      '    poll_timeout: 30s',
      '}',
      '',
      'pipeline Integrate {',
      '    step Fetch timeout 45s {',
      '        var issues = Issues.call("search_issues", {query: "is:open"})',
      '        var reply = Translator.send("Translate hello to French")',
      '        log(reply.text)',
      '    }',
      '}'
    ], "A resposta depende dos serviços configurados.\nSubstitua os endereços example.com e configure MCP_TOKEN antes de executar.",
    ["MCP chama ferramentas; A2A envia mensagens para agentes remotos.", "Para operações diretas, o runtime também fornece http, fs, cmd, git, json, time e uuid. Consulte a referência para os contratos."],
    "Adapte uma extensão para um serviço do seu ambiente e valide com mhl lint antes de executar.",
    "sample/features/a2a/a2a_send_calls_the_real_api.mh",
    { requirements: "Serviços MCP/A2A reais e MCP_TOKEN no ambiente. Os endereços são ilustrativos.", external: true });

  if (typeof module !== "undefined" && module.exports) module.exports = lessons;
  else root.MhlLessons = lessons;
})(globalThis);
