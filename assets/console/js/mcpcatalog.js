// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0
// Ready-made MCP settings. Each is only a starting point: the form opens filled
// in and every field can be changed. "local" = a program on your machine
// (needs your machine; never on a team server); "url" = a web MCP.
// `onlyHere` marks a URL that points at an app on your own machine.

export const CATALOG = [
  { id: 'blender', name: 'Blender', group: 'apps', kind: 'local', hue: 28,
    command: 'uvx', args: ['blender-mcp'], docs: 'https://github.com/ahujasid/blender-mcp' },
  { id: 'roblox', name: 'Roblox Studio', group: 'apps', kind: 'local', hue: 210,
    command: 'rbx-studio-mcp', args: ['--stdio'], docs: 'https://github.com/Roblox/studio-rust-mcp-server' },
  { id: 'godot', name: 'Godot', group: 'apps', kind: 'local', hue: 200,
    command: 'npx', args: ['-y', '@coding-solo/godot-mcp'], env: ['GODOT_PATH'], docs: 'https://github.com/Coding-Solo/godot-mcp' },
  { id: 'figma', name: 'Figma desktop', group: 'apps', kind: 'url', onlyHere: true, hue: 330,
    url: 'http://127.0.0.1:3845/mcp', docs: 'https://help.figma.com/' },
  { id: 'playwright', name: 'Playwright', group: 'dev', kind: 'local', hue: 140,
    command: 'npx', args: ['@playwright/mcp@latest'], docs: 'https://github.com/microsoft/playwright-mcp' },
  { id: 'files', name: 'Folder', group: 'dev', kind: 'local', hue: 45,
    command: 'npx', args: ['-y', '@modelcontextprotocol/server-filesystem', '<folder>'], docs: 'https://github.com/modelcontextprotocol/servers' },
  { id: 'context7', name: 'Context7', group: 'web', kind: 'url', hue: 160,
    url: 'https://mcp.context7.com/mcp', docs: 'https://github.com/upstash/context7' },
  { id: 'deepwiki', name: 'DeepWiki', group: 'web', kind: 'url', hue: 250,
    url: 'https://mcp.deepwiki.com/mcp', docs: 'https://docs.devin.ai/work-with-devin/deepwiki-mcp' },
  { id: 'github', name: 'GitHub', group: 'web', kind: 'url', token: true, hue: 270,
    url: 'https://api.githubcopilot.com/mcp/', docs: 'https://github.com/github/github-mcp-server' },
];

export const GROUPS = ['apps', 'dev', 'web'];
