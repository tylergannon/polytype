// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	site: 'https://go-gen-jsonschema.tylergannon.com',
	redirects: {
		'/spec/v1': '/spec/',
		'/guides/example': '/getting-started/',
		'/reference/example': '/reference/cli/',
		'/implementation': '/spec/',
	},
	vite: {
		server: { fs: { allow: ['..', '../..'] } },
	},
	integrations: [
		starlight({
				title: 'polytype',
				description: 'Generate deterministic, LLM-ready JSON Schema from Go types.',
				logo: { src: './src/assets/gopher-front.svg', replacesTitle: true },
				favicon: '/favicon.svg',
				social: [
					{ icon: 'github', label: 'GitHub', href: 'https://github.com/tylergannon/polytype' },
				],
				customCss: [
					'./src/styles/custom.css'
				],
				sidebar: [
					{
						label: 'Start here',
						items: [
							{ label: 'Overview', link: '/' },
							{ label: 'Getting started', link: '/getting-started/' },
							{ label: 'Examples', link: '/examples/' },
						],
					},
					{
						label: 'Core features',
						items: [
							{ label: 'Optional and nullable', link: '/features/optionality/' },
							{ label: 'Enums', link: '/features/enums/' },
							{ label: 'Interfaces', link: '/features/interfaces/' },
							{ label: 'Shared definitions (AsRef)', link: '/features/ref-types/' },
							{ label: 'Provider schemas', link: '/features/providers/' },
						],
					},
					{
						label: 'Production use',
						items: [
							{ label: 'Validation and CI', link: '/guides/validation-ci/' },
							{ label: 'devalue transport (SvelteKit)', link: '/guides/devalue/' },
							{ label: 'CLI reference', link: '/reference/cli/' },
						],
					},
					{
						label: 'Extending',
						items: [
							{ label: 'Custom backends', link: '/guides/custom-backends/' },
						],
					},
					{
						label: 'Reference',
						items: [
							{ label: 'Go API', link: '/api/' },
							{ label: 'Specification', link: '/spec/' },
						],
					},
				],
		}),
	],
});
