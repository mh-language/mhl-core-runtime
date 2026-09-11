/* English teaching material shares stable lesson IDs and program structure with pt-BR. */
(function (root) {
  "use strict";
  const groups = {
    "Fundamentos": "Foundations",
    "Orquestração": "Orchestration",
    "Concorrência e limites": "Concurrency and limits",
    "Resiliência e integrações": "Resilience and integrations"
  };
  const concepts = {
    "interpolação": "interpolation", "condição de parada": "stop condition",
    "parâmetros padrão": "default parameters", "argumentos nomeados": "named arguments",
    "duração": "duration", "barreira": "barrier"
  };
  const requirements = {
    "Somente o runtime MHL.": "Only the MHL runtime.",
    "Runtime MHL e o executável echo no PATH.": "The MHL runtime and the echo executable on PATH.",
    "Runtime MHL e echo no PATH.": "The MHL runtime and echo on PATH.",
    "Runtime MHL; a execução cria um arquivo JSON em .mhl/.": "The MHL runtime; running this example creates a JSON file in .mhl/.",
    "Serviços MCP/A2A reais e MCP_TOKEN no ambiente. Os endereços são ilustrativos.": "Real MCP/A2A services and MCP_TOKEN in the environment. The URLs are placeholders."
  };
  const translations = {
    valores: {
      title: "Values and variables",
      description: "Create a complete program. Executable statements belong inside a step; var can change and const cannot be reassigned.",
      result: "mhl: attempt 2 of 3",
      notes: ["Interpolation with ${expression} includes values in strings.", "A variable declared inside a step belongs to that step."],
      challenge: "Change the project name and increment attempt once more. What will the message be?",
      replacements: [["tentativa ", "attempt "], [" de ", " of "]]
    },
    colecoes: {
      title: "Collections and optional data",
      description: "Filter objects, transform arrays with lambdas, and supply defaults for missing fields.",
      result: "team: lint, deploy",
      notes: ["map and filter return new arrays.", "?. provides optional access; ?? uses the right-hand value when the left-hand value is null."],
      challenge: "Filter only priority > 2 and add owner to the config object."
    },
    decisoes: {
      title: "Types, enum and match",
      description: "Name types and possible states. Combine a boolean decision with an exhaustive selection.",
      result: "With --input score=8:\npassed",
      notes: ["type creates an alias, not a distinct type.", "mhl lint checks that match covers the enum variants."],
      challenge: "Run with score=5. Then remove a match arm and run mhl lint.",
      replacements: [['"aguardando"', '"pending"'], ['"aprovado"', '"passed"'], ['"reprovado"', '"failed"']]
    },
    lacos: {
      title: "for and while",
      description: "Iterate over a collection or repeat an operation with an explicit stop condition.",
      result: "total=8; attempts=2",
      notes: ["while repeats statements within a step. loop pipeline repeats the sequence of steps.", "MHL has no continue statement. break ends the pipeline execution, not just the nearest for or while loop."],
      challenge: "Remove the n != 2 condition and predict the new total before running the program.",
      replacements: [["tentativas=", "attempts="]]
    },
    "ferramentas-testes": {
      title: "Tools and tests",
      description: "Extract a typed operation and record executable examples using the language's assertions.",
      result: "2 test cases should pass with mhl test.",
      notes: ["test and describe isolate verifiable examples.", 'Imports let you share declarations: import {Text} from "text.mh" (export Text from that module).'],
      challenge: "Add a describe block that checks a word with no spaces."
    },
    pipeline: {
      title: "Inputs, steps and output",
      description: "Share state between steps and expose an explicit result contract.",
      result: 'With --input diff=password:\nmanual review\nOutput contract: {risk: "high"}',
      notes: ["Declare var in the pipeline body to share its value across steps.", "output projects only the fields the caller should receive."],
      challenge: "Add an internal variable and check that it does not appear in the output contract."
    },
    agentes: {
      title: "Prompts and agents",
      description: "Separate the parameterized instruction from the agent's call configuration. Echo lets you test the connection without an AI API.",
      result: "Review api.go. Focus on security.",
      notes: ["Echo returns the prompt; command/args can point to a model CLI.", "Agent calls happen in the local runtime. The browser does not start processes."],
      challenge: 'Pass focus: "performance" to Review. This prompt uses named arguments.'
    },
    roteador: {
      title: "Router and delegating between agents",
      description: "Declare a router over already-declared agents and decide which one handles a prompt: first through a deterministic rule, then, if needed, through a decision call.",
      result: 'billing-said: my invoice is wrong\n\nWithout "invoice" in the prompt, select returns null and Frontdesk decides through a decision call to the Judge agent (here, echo stands in for that decision).',
      notes: ["select runs first; a return that matches one of the names in agents skips the decision call.", "Without select, or when it can't decide, delegate runs the decider (an ordinary, separately declared agent) with a decision call, then runs the chosen agent with the same prompt.", "decider is optional: omit it entirely for a purely deterministic router — in that case select alone must cover every prompt.", "nameof(Billing), instead of the string \"Billing\", turns a typo into an mhl lint error and makes the editor's \"go to definition\" work; a return that matches no declared agent always fails immediately — it's never treated as select being undecided."],
      challenge: 'Change the prompt so it doesn\'t mention "invoice" and predict which agent answers.'
    },
    "loop-max": {
      title: "Execution limits",
      description: "Repeat the entire pipeline with an explicit iteration ceiling. mem retains its value between iterations.",
      result: "round 1\nround 2\nround 3",
      notes: ["max 3 is equivalent to repeat: { max_iterations: 3 }. Use one form.", "This limit counts complete iterations. break ends execution early, skipping remaining steps and further iterations."],
      challenge: "Change max 3 to max 2 and add a second step. How many steps will run?",
      replacements: [["rodada ", "round "], ['"suficiente"', '"enough"']]
    },
    "loop-condicao": {
      title: "Loop until a condition",
      description: "Use an observable condition together with a safety ceiling, rather than relying only on a fixed repetition count.",
      result: "check 1: pending\ncheck 2: pending\ncheck 3: done",
      notes: ["stop_when can read mem or a memory store. Step-local variables are outside its scope.", "The runtime ends the loop when the condition is satisfied or the ceiling is reached."],
      challenge: "Replace checks >= 3 with checks >= 8. The ceiling of 5 prevents a sixth iteration."
    },
    timeout: {
      title: "Step timeout",
      description: "Limit how long a step's calls may take. time.sleep makes the deadline's effect easy to reproduce.",
      result: "ready\nfinished\n\nExperiment: with time.sleep(3s), Prepare exceeds 2s and Finish does not run.",
      notes: ["A step timeout is declared without a colon: step Prepare timeout 2s.", "The deadline cancels blocking calls. On resume, the step receives a fresh deadline; this does not increase max_iterations."],
      challenge: "Change the sleep to 3s. Run it in the CLI to observe the timeout failure."
    },
    "spawn-wait": {
      title: "spawn, wait and concurrency",
      description: "Start concurrent calls within a step and wait for their results with an explicit deadline.",
      result: "docs ready\ntests ready\nsecurity ready\n\nAt most 2 agent calls in flight; wait joins all 3.",
      notes: ["max_concurrency caps in-flight calls across the run. wait timeout: limits the wait; step timeout bounds the entire step.", "Handles expose .result, .status, .ok and .error. Calls that have not been awaited are joined when the step ends."],
      challenge: "Set max_concurrency to 1. All three tasks still exist, but the calls run sequentially."
    },
    "fan-out": {
      title: "Fan-out and wait policies",
      description: "Start one call per item and choose whether to wait for all results, the first success, or a quorum.",
      result: '3 handles, one per file.\nwait checks: joins all.\nwait any: first success.\nwait 2 of: two successes.\non_error: "collect": inspect each handle’s .ok and .error.',
      notes: ["any and quorum stop waiting once enough calls succeed and cancel the remaining tasks.", "collect lets you inspect individual failures. It does not guarantee that every agent succeeds."],
      challenge: "Replace the active wait with wait any. Check .ok before using .result from cancelled tasks.",
      replacements: [["Alternativas ao wait acima:", "Alternatives to the wait above:"]]
    },
    parallel: {
      title: "Parallel steps",
      description: "Run entire steps in parallel, with individual deadlines and a deadline for the group's barrier.",
      result: "Gather → Docs + Issues → barrier → Merge\ndocs ready; issues ready",
      notes: ["The earliest deadline wins, whether it belongs to the group or a step.", "Writes merge at the barrier; conflicting assignments to the same variable fail. Resuming reruns the group."],
      challenge: "Put time.sleep(3s) in Issues and observe the group timing out before Merge."
    },
    memoria: {
      title: "Memory and sessions",
      description: "Persist data explicitly and use the session ID to keep runs separate.",
      result: "reviewed\n\nThe CLI writes .mhl/session.<session-id>.json.",
      notes: ["memory is an explicit store; mem is state that survives loop iterations.", "context.session_id and context.started_at are available without declaring context:."],
      challenge: "Save and read an object with two fields in the same session."
    },
    retomada: {
      title: "Workflows and human approval",
      description: "Branch with goto and suspend execution until an external decision arrives. The checkpoint lets you resume the step.",
      result: "With approved=false: execution pauses in Gate.\nResume with approved=true: Gate runs again and Publish prints approved.",
      notes: ["goto is allowed in workflow, not in pipeline.", "pause is a suspension signal, not an exception caught by try/catch. Avoid non-idempotent effects before pausing."],
      challenge: "Run with approved=false, then use the resume command with approved=true.",
      replacements: [["Aguardando aprovação", "Awaiting approval"]]
    },
    falhas: {
      title: "Retries and error handling",
      description: "Set the agent's attempt ceiling and handle the error when the call cannot produce a result.",
      result: "ok\n\nA successful call does not consume the remaining attempts.",
      notes: ["max_attempts includes the initial attempt. The step deadline also bounds the call and its retries.", 'cache: { strategy: "exact", ttl: 1h } allows response reuse; consider whether the prompt and side effects make caching appropriate.'],
      challenge: "Use a failing agent in your environment to observe retry and catch.",
      replacements: [["revisão indisponível", "review unavailable"]]
    },
    integracoes: {
      title: "MCP, A2A and external services",
      description: "Connect MCP tools and A2A agents using declarative configuration and environment credentials.",
      result: "The response depends on the configured services.\nReplace the example.com URLs and set MCP_TOKEN before running.",
      notes: ["MCP calls tools; A2A sends messages to remote agents.", "For direct operations, the runtime also provides http, fs, cmd, git, json, time and uuid. See the reference for their contracts."],
      challenge: "Adapt an extension to a service in your environment and validate with mhl lint before running."
    }
  };
  function localize(lessons) {
    return lessons.map(lesson => {
      const entry = translations[lesson.id];
      if (!entry) throw new Error("Missing English lesson: " + lesson.id);
      const { replacements = [], ...text } = entry;
      let code = lesson.code;
      for (const [from, to] of replacements) {
        if (!code.includes(from)) throw new Error("Outdated English code translation: " + lesson.id);
        code = code.replaceAll(from, to);
      }
      if (!requirements[lesson.requirements] || !groups[lesson.group]) throw new Error("Missing English lesson metadata: " + lesson.id);
      return { ...lesson, ...text, code, group: groups[lesson.group],
        concepts: lesson.concepts.map(value => concepts[value] || value),
        requirements: requirements[lesson.requirements] };
    });
  }
  if (typeof module !== "undefined" && module.exports) module.exports = localize;
  else root.MhlLessons = localize(root.MhlLessons);
})(globalThis);
