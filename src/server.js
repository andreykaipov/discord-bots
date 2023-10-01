/**
 * The core server that runs on a Cloudflare worker.
 */

import { error, json, Router } from 'itty-router'
import * as commands from './commands.js'
import * as middleware from './middleware.js'
import { table, getBorderCharacters } from 'table'
import * as timeago from 'timeago.js'
import {
    InteractionResponseType,
    InteractionType,
    InteractionResponseFlags,
} from 'discord-interactions'

import { en_short as locale_en_short } from 'timeago.js/lib/lang/index.js'
timeago.register('en_short', locale_en_short)

const router = Router()
    .all('*', ...Object.values(middleware))
    .get('/', (request, env) => {
        return new Response(`👋 ${env.DISCORD_APPLICATION_ID}`)
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
            case commands.STARS_COMMAND.name.toLowerCase(): {
                const response = await fetch('https://map.starminers.site/data')
                const stars = (await response.json()).map((star) => ({
                    called: star.calledAt,
                    tier: star.tier,
                    world: star.world,
                    location: star.calledLocation,
                    calledBy: star.calledBy,
                }))
                stars.sort((a, b) => b.called - a.called)
                const formatted = stars.map((star) => {
                    star.called = timeago.format(star.called * 1000, 'en_short')
                    return star
                })
                const text = table(
                    [
                        Object.keys(stars[0]),
                        ...formatted.map(Object.values).slice(0, 20),
                    ],
                    {
                        border: getBorderCharacters('void'),
                        columnDefault: {
                            paddingLeft: 0,
                            paddingRight: 1,
                        },
                        drawHorizontalLine: (lineIndex, rowCount) => {
                            return lineIndex === 0 || lineIndex === rowCount
                        },
                        drawVerticalLine: (lineIndex, columnCount) => {
                            return lineIndex === 0 || lineIndex === columnCount
                        },
                    },
                )
                return {
                    type: InteractionResponseType.CHANNEL_MESSAGE_WITH_SOURCE,
                    data: {
                        content: `
\`\`\`
${text}
\`\`\`
            `,
                    },
                }
            }
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
        router
            .handle(request, env, ctx)
            .then(json) // send as JSON
            .catch(error), // catch errors
}

export default server
