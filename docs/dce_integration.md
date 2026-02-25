Refined Mermaid Diagram: Developer Interaction with VS Code Extension
```mermaid
graph TD
    A[Developer] -->|Interacts with| B[VS Code Extension]

    %% Path 1: Generating PRs on Commit
    B -->|Post-Commit Trigger| C1[Post-Commit Hook]
    C1 -->|Invokes| D1[prbuddy-go Backend]
    D1 -->|Starts Persistent Conversation| E1[Persisted PR Context]
    E1 -->|Sends Request to LLM| F1[LLM Generates Draft PR]
    F1 -->|Provides Draft| G1[Developer Iterates on PR]
    G1 -->|Updates Context| E1

    %% Path 2: Using QuickAssist
    B -->|QuickAssist Request| C2[QuickAssist Activation]
    C2 -->|Calls Backend| D2[prbuddy-go Backend]
    D2 -->|Creates Ephemeral Context| E2[Ephemeral Context]
    E2 -->|Sends Query to LLM| F2[LLM Processes Query]
    F2 -->|Provides Response| G2[Quick Response to Developer]

    %% Path 3: Activating DCE
    B -->|Toggles DCE On| C3[DCE Toggle]
    C3 -->|Starts Feedback Loop| H3[DCE Loop Initialized]
    H3 -->|Prompts Developer| F3[Prompt: What are we doing today?]
    F3 -->|Receives Input| G3[Developer Provides Task List]
    G3 -->|Processes Task List| I3[DCE Builds Dynamic Context]
    I3 -->|Augments Context| E2[DCE Enhances QuickAssist]
    E2 -->|Dynamic Query Handling| F2
    F2 -->|Feedback| H3[Feedback Loop]

    %% Styling for Clarity
    classDef path1 fill:#f9f,stroke:#333,stroke-width:2px;
    classDef path2 fill:#bbf,stroke:#333,stroke-width:2px;
    classDef path3 fill:#bfb,stroke:#333,stroke-width:2px;

    class C1,D1,E1,F1,G1 path1;
    class C2,D2,E2,F2,G2 path2;
    class C3,H3,F3,G3,I3 path3;


```

---

```mermaid
graph TD
    A[Developer] --> B[VS Code Extension]

    %% Path 1: Generating PRs on Commit
    B --> C1[Post-Commit Hook Trigger]
    C1 --> D1[prbuddy-go Backend]
    D1 --> E1[Start PR Conversation]
    E1 --> F1[LLM Generates Draft PR]
    F1 --> G1[Developer Iterates on PR]
    G1 --> D1

    %% Path 2: Using QuickAssist
    B --> C2[QuickAssist Activation]
    C2 --> D2[QuickAssist Endpoint]
    D2 --> E2[Create Ephemeral Context]
    E2 --> F2[LLM Processes Query]
    F2 --> G2[Quick Response to Developer]

    %% Path 3: Activating DCE
    B --> C3[DCE Toggle On]
    C3 --> D3[DCE Endpoint]
    D3 --> H3[DCE Loop Initialized]
    H3 --> F3[Prompt: What are we doing today?]
    F3 --> G3[Developer Provides Task List]
    G3 --> I3[DCE Builds Dynamic Context]
    I3 --> E2[DCE Augments QuickAssist]
    E2 --> F2
    F2 --> H3[Dynamic Feedback Loop]

    %% Styling for clarity
    classDef path1 fill:#f9f,stroke:#333,stroke-width:2px;
    classDef path2 fill:#bbf,stroke:#333,stroke-width:2px;
    classDef path3 fill:#bfb,stroke:#333,stroke-width:2px;

    class C1,D1,E1,F1,G1 path1;
    class C2,D2,E2,F2,G2 path2;
    class C3,D3,H3,F3,G3,I3 path3;
```


## Function Extraction

The DCE uses Tree-sitter for accurate Go function extraction. 

**Limitations**:
- Go files only (`.go` extension)
- Requires valid Go syntax (parse errors = empty function list)
- First run parses entire repo (subsequent runs benefit from caching)

**Path Normalization**:
Tree-sitter may return absolute paths while git returns relative paths.
The `normalizeFilePath()` function handles this automatically.