import {
    InteractionResponseType,
    InteractionType,
    InteractionResponseFlags,
} from 'lib'
import { client } from 'lib'

export const commands = async (req, env) => {
    console.log('handling commands')
    const interaction = req.content

    if (interaction.type === InteractionType.APPLICATION_COMMAND) {
        const cmd = interaction.data.name.toLowerCase()
        console.log('handling command', cmd)

        switch (interaction.data.name.toLowerCase()) {
            case 'ping': {
                return {
                    type: InteractionResponseType.CHANNEL_MESSAGE_WITH_SOURCE,
                    data: {
                        content: 'Pong!',
                    },
                }
            }
            case 'test': {
                const channel = interaction.channel.id
                const resp = await discord(env)(
                    'POST',
                    `channels/${channel}/messages`,
                    'These oils are harming your health and energy, here’s why 🤯',
                )
                console.log(env.DISCORD_TOKEN)
                console.log(resp.status, await resp.json())
            }
        }
    }
    return {}
}
