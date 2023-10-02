import { json, error, Router } from 'itty-router'
import { middleware } from './middleware.js'
import { getStars, updateChannels } from './stars.js'

const router = Router()
    .all('*', ...middleware)
    .get('/', (request, env) => {
        return { '👋': `${env.DISCORD_APPLICATION_ID}` }
    })
    .post('/message/:channel_id', async (request) => {
        const id = request.params.channel_id
        return request.discord(
            'POST',
            `channels/${id}/messages`,
            request.content,
        )
    })
    //.all('/test', (_, env) => updateChannels(env, env.channels))
    .all('*', () => new Response('Not Found.', { status: 404 }))

const server = {
    fetch: (request, env, ctx) =>
        router.handle(request, env, ctx).then(json).catch(error),
    scheduled: async (event, env) => {
        const channels = env.channels
        console.log(channels)
        await updateChannels(env, channels)
    },
}

export default server
