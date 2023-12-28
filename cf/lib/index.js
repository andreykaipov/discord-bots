export {
    InteractionResponseType,
    InteractionType,
    InteractionResponseFlags,
} from 'discord-interactions'

export const discord =
    (env) =>
    async (method, path, body = null) => {
        if (body && typeof body === 'string') {
            body = JSON.stringify({ content: body })
        }
        if (body && typeof body !== 'string') {
            body = JSON.stringify(body)
        }
        const url = `https://discord.com/api/${path}`
        const response = await fetch(url, {
            method: method,
            body: body,
            headers: {
                Authorization: `Bot ${env.DISCORD_TOKEN}`,
                'Content-Type': 'application/json',
                Accept: 'application/json',
            },
        })
        return response
    }

export const chunk = (arr, size = arr.length) => {
    if (arr.length <= size) {
        return [arr]
    }
    return [arr.slice(0, size), ...chunk(arr.slice(size), size)]
}

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

import { json, error, Router as IttyRouter } from 'itty-router'
import * as middleware from './middleware.js'
export const router = (slashCommandHandler) => {
    return IttyRouter()
        .all('*', ...middleware.common)
        .get('/', (request, env) => {
            return { '👋': `${env.DISCORD_APPLICATION_ID}` }
        })
        .post(
            '/',
            middleware.verifyDiscordKey,
            //middleware.withDetail,
            async (request, env) => slashCommandHandler(request, env),
        )
        .post('/message/:channel_id', async (request) => {
            const id = request.params.channel_id
            return request.discord(
                'POST',
                `channels/${id}/messages`,
                request.content,
            )
        })
        .all('*', () => new Response('Not Found.', { status: 404 }))
}

export const server = (slashCommandHandler) => {
    return {
        fetch: (request, env, ctx) =>
            router(slashCommandHandler)
                .handle(request, env, ctx)
                .then(json)
                .catch(error),
        scheduled: async (event, env) => {
            console.log('scheduled event...')
        },
    }
}

// This wraps the command handler with common logic so that bots only have
// to pass the following:
//
// handler: (cmd, interaction, env) => Response
//
// And get back a handler:
//
// (req, env) => Response
import { InteractionType } from 'discord-interactions'
export const commandHandlerFunc = (handler) => {
    return async (req, env) => {
        const interaction = req.content

        if (interaction.type !== InteractionType.APPLICATION_COMMAND) {
            return new Response('Unhandled interaction type', {
                status: 400,
                type: interaction.type,
            })
        }

        const cmd = interaction.data.name.toLowerCase()

        return await handler(cmd, interaction, env)
    }
}
