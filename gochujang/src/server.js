import { json, error, Router } from 'itty-router'
import * as middleware from 'lib/middleware.js'
import * as handle from './handle.js'

const router = Router()
    .all('*', ...middleware.common)
    .get('/', (request, env) => {
        return { '👋': `${env.DISCORD_APPLICATION_ID}` }
    })
    .post(
        '/',
        middleware.verifyDiscordKey,
        middleware.withDetail,
        async (request, env) => {
            return await handle.commands(request, env)
        },
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

const server = {
    fetch: (request, env, ctx) =>
        router.handle(request, env, ctx).then(json).catch(error),
    scheduled: async (event, env) => {
        console.log('scheduled event...')
    },
}

export default server
