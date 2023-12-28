import { InteractionResponseType } from 'lib'
import { commandHandlerFunc, sleep, discord } from 'lib'
import * as copypasta from './copypasta.js'

export const commands = commandHandlerFunc(async (cmd, interaction, env) => {
    switch (cmd) {
        case 'ping': {
            return {
                type: InteractionResponseType.CHANNEL_MESSAGE_WITH_SOURCE,
                data: { content: 'Pong!' },
            }
        }
        case 'thanks': {
            return {
                type: InteractionResponseType.CHANNEL_MESSAGE_WITH_SOURCE,
                data: { content: copypasta.thanks },
            }
        }
        default: {
            return {
                type: InteractionResponseType.CHANNEL_MESSAGE_WITH_SOURCE,
                data: { content: 'Unhandled command' },
            }
        }
    }
})
