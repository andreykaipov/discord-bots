/**
 * The core server that runs on a Cloudflare worker.
 */

import { json, error, Router } from 'itty-router'
import * as commands from './commands.js'
import { middleware } from './middleware.js'
import { getStars } from './stars.js'
import {
    InteractionResponseType,
    InteractionType,
    InteractionResponseFlags,
} from 'discord-interactions'
import { sleep, discord } from './helpers.js'

const router = Router()
    .all('*', ...middleware)
    .get('/', (request, env) => {
        return { hello: `👋 ${env.DISCORD_APPLICATION_ID}` }
    })

router.post('/message/:channel_id', async (request) => {
    const id = request.params.channel_id
    return request.discord('POST', `channels/${id}/messages`, request.content)
})

/**
 * A simple :wave: hello page to verify the worker is working.
 */

/**
 * Main route for all requests sent from Discord.  All incoming messages will
 * include a JSON payload described here:
 * https://discord.com/developers/docs/interactions/receiving-and-responding#interaction-object
 */
router.post('/', async (request, env) => {
    const { isValid, interaction } = await request.verifyDiscordRequest()
    if (!isValid || !interaction) {
        return new Response('Bad request signature.', { status: 401 })
    }

    if (interaction.type === InteractionType.PING) {
        // The `PING` message is used during the initial webhook handshake, and is
        // required to configure the webhook in the developer portal.
        return {
            type: InteractionResponseType.PONG,
        }
    }

    if (interaction.type === InteractionType.APPLICATION_COMMAND) {
        // Most user commands will come as `APPLICATION_COMMAND`.
        switch (interaction.data.name.toLowerCase()) {
            case commands.INVITE_COMMAND.name.toLowerCase(): {
                const applicationId = env.DISCORD_APPLICATION_ID
                const INVITE_URL = `https://discord.com/oauth2/authorize?client_id=${applicationId}&scope=applications.commands`
                return {
                    type: InteractionResponseType.CHANNEL_MESSAGE_WITH_SOURCE,
                    data: {
                        content: INVITE_URL,
                        flags: InteractionResponseFlags.EPHEMERAL,
                    },
                }
            }
            default:
                return error(400, 'unknown type')
        }
    }

    console.error('Unknown Type')
    return error(400, 'unknown type')
})

router.all('*', () => new Response('Not Found.', { status: 404 }))

const server = {
    fetch: (request, env, ctx) =>
        router.handle(request, env, ctx).then(json).catch(error),
    scheduled: async (event, env) => {
        console.log('start of scheduled')
        const channels = ['1136705165555159060']
        const stars = await getStars()
        const starlines = stars.match(/(?=[\s\S])(?:.*\n?){1,20}/g) // split every 20th new line
        console.log(starlines)
        for await (const channel of channels) {
            const response = await discord(env)(
                'GET',
                `channels/${channel}/messages?limit=100`,
            )
            const messages = await response.json()
            console.log(messages.length)
            for await (const message of messages) {
                const response = await discord(env)(
                    'DELETE',
                    `channels/${channel}/messages/${message.id}`,
                )
                console.log(response.status)
                await sleep(500)
            }

            for (const chunk of starlines) {
                const body = { content: `\`\`\`${chunk}\`\`\`` }
                await discord(env)('POST', `channels/${channel}/messages`, body)
            }
        }
        console.log('end of scheduled')
    },
}

export default server
