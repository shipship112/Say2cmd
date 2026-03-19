# Say2cmd

Intelligent Cmd! A command-line tool that converts natural language into Cmd commands.

## Features

- Convert natural language into Cmd commands
- Automatically execute generated commands
- Provide explanations for generated commands
- Cross-platform support
- Streaming response for better user experience
- Environment awareness to obtain current directory file status
- Intelligent error handling: automatically regenerate scripts when execution fails
- Support for multilingual output (Chinese/English)
- Support for various AI model providers (OpenAI, Alibaba Cloud)

## Working Principle

1. **User Input**: The user enters a task described in natural language
2. **Environment Awareness**: The tool obtains the file and directory status of the current directory
3. **AI Processing**: Send user input and environment information to the AI model
4. **Response Parsing**: Parse chat completion responses (supports streaming SSE), splice `choices[].delta.content` in real time (compatible with `message.content` / `output.text`), and extract the final single-line command.
5. **Interactive Execution**: Display the generated command; users can choose to execute, adjust further (regenerate after supplementing information), or cancel.
6. **Failure Closed-Loop**: If command execution fails, automatically feed back error information, exit code and output to the model, and regenerate a more reliable command.
