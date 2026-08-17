export const enAgentGuiHomeSuggestions = {
  homeSuggestionsClose: "Close suggestions",
  homeSuggestions: {
    about: {
      title: "Meet Tutti",
      prompt: "Tell me what Tutti can help me do"
    },
    cloneGithubRepository: {
      title: "Clone GitHub repository",
      prompt:
        "Help me clone a GitHub repository. First ask for the repository URL and destination directory. If it is private, help me verify GitHub authentication or access before cloning. Proceed only after you have the required information."
    },
    breakdown: {
      title: "Task breakdown",
      taskCenterLabel: "Task management",
      prompt:
        "Use {{taskCenterMention}} to help me break down the task, topic { enter here }"
    },
    review: {
      title: "Quality review",
      prompt: "Have { @agent } review the output quality of { @agent session }"
    },
    interaction: {
      title: "Agent interaction",
      prompt:
        "Have { @agent } and { @agent } work together to { do something }, topic { enter here }"
    },
    import: {
      title: "Import session"
    }
  }
} as const;
