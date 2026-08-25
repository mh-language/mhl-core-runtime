Initialize the development run from this brief by following the injected `dev-initializer` skill:

## 1. Classify the target

Determine whether the target is:

- **Greenfield:** no existing application. Use `app/<descriptive-name>` as the target and create the minimal project structure.

- **Brownfield:** an application already exists. 
  - Inspect only the metadata needed to plan the requested delta: 
    - the top-level layout, 
    -README, 
    - build manifests, 
    - existing docs/ADRs, 
    - bootstrap or verification scripts, and 
    - `progress.txt`. 
  - Reuse the established target and verification command. 
  - Do not inventory the whole source tree or re-plan the existing app.

## 2. Read the brief
Read the brief and extract the requirements, constraints, and acceptance criteria. Identify the distinct capabilities that must be implemented to satisfy the brief. Each capability should correspond to a single feature in the plan.

<brief>${content}</brief>

## 3. Plan features

For greenfield, split the whole goal. For brownfield, split only the requested delta. Makeeach feature:

- small, vertical, independently implementable, and independently verifiable;
- assigned a numeric priority where `1` is highest;
- linked through `dependsOn` only when a real implementation dependency exists;
- described objectively with enough context for a fresh session;
- linked through `references` to every explicit code that scopes the feature, including requirements, ADRs, acceptance criteria, interfaces, data rules, and controls (`RF-*`, `ADR-*`, `AC-*`, `IF-*`, `RN-*`, `IC-*`) when those artifacts are present;
- populated with `implementationContext` as an object containing the bounded inline material the feature needs to be implemented without reopening the full brief. Use the five arrays `requirements`, `decisions`, `constraints`, `files`, and `acceptance`; copy only the relevant requirements, ADR/decision descriptions, constraints, target files, examples, and acceptance criteria into those arrays.

Prefer several small features to a few broad ones. If a feature has no unambiguous verification path, split it further. A single "scaffold everything" feature covering the whole brief is not a valid plan for a multi-requirement goal — each distinct capability the brief describes (each endpoint, each user-facing operation, each explicitly named non-functional slice) becomes its own feature, even when that means ten-plus entries.

## What ReadinessEvaluator checks (when verdict is READY)

- Slice ids are unique.
- Every `requirementIds` entry resolves to a real requirement in the accepted SRS; every
  `adrIds` entry resolves to a real ADR in the accepted SDD.
- Every `dependsOn` entry names another slice in this same proposal — never itself, never an
  id outside the list.
- The `dependsOn` graph is acyclic, and at least one slice has an empty `dependsOn` (a
  starting slice with no prerequisite).
- Every requirement in the accepted SRS is covered by at least one slice's `requirementIds` —
  the slices must form a complete cover; nothing may be left untraced.

## 4. Write the plan

**schema**: Then follow schema represents the structure of the feature list to be written to `${plan_path}`. Each feature is born with `status` set to `"pending"`.:  
```json
"{\"$schema\":\"https://json-schema.org/draft/2020-12/schema\",\"$id\":\"feature_list.schema.json\",\"title\":\"FeatureList\",\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"features\"],\"properties\":{\"features\":{\"type\":\"array\",\"items\":{\"$ref\":\"#/$defs/feature\"}}},\"$defs\":{\"feature\":{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"dependsOn\",\"description\",\"id\",\"implementationContext\",\"priority\",\"references\",\"status\",\"title\"],\"properties\":{\"dependsOn\":{\"type\":\"array\",\"items\":{\"type\":\"integer\"},\"description\":\"Ids of features that must complete before this one.\"},\"description\":{\"type\":\"string\"},\"id\":{\"type\":\"integer\"},\"implementationContext\":{\"$ref\":\"#/$defs/implementationContext\"},\"priority\":{\"type\":\"integer\"},\"references\":{\"type\":\"array\",\"items\":{\"type\":\"string\"},\"description\":\"Flat list of requirement/ADR/AC/interface/design/IC ids cited by this feature.\"},\"status\":{\"type\":\"string\",\"enum\":[\"pending\",\"in_progress\",\"completed\",\"blocked\"]},\"title\":{\"type\":\"string\"}}},\"implementationContext\":{\"type\":\"object\",\"additionalProperties\":false,\"required\":[\"acceptance\",\"constraints\",\"decisions\",\"files\",\"requirements\"],\"properties\":{\"acceptance\":{\"type\":\"array\",\"items\":{\"type\":\"string\"}},\"constraints\":{\"type\":\"array\",\"items\":{\"type\":\"string\"}},\"decisions\":{\"type\":\"array\",\"items\":{\"type\":\"string\"}},\"files\":{\"type\":\"array\",\"items\":{\"type\":\"string\"}},\"requirements\":{\"type\":\"array\",\"items\":{\"type\":\"string\"}}}}}}"
```

Write a JSON ARRAY to the file `${plan_path}` (a real file, written with your file-write tool — NOT escaped or embedded inside the envelope you send back).