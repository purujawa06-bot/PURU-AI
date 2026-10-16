This is human handwriting. Thank you for using PURU-AI / PURUCLAW, I USED REFERENCES FROM 2 POPULAR PUBLIC REPOSITORIES, NAMELY PICOCLAW AND OPENCLAW.

This final commit is because I've verified that context management, memory, agent memory, and everything else are ready for daily use. I think the project is mature here. 

October 7, 2026: Agents can receive document files and save them to the workspace and inject their absolute path into the prompt. 

October 16, 2026: `puru setup` became a real wizard. Questions are grouped in sections with colors (auto-plain when piped, NO_COLOR, or not a TTY), secrets are masked in a preview panel before the file is written, and every field defaults to the existing config.json, so pressing Enter keeps the current setup (env vars still win, and the minimal wizard carries over the rest). Shipped as v6.1.0 on GitHub Releases and npm.
