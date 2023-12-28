import * as handle from './handle.js'
import * as lib from 'lib'

export default {
    ...lib.server(handle.commands),
    scheduled: async (event, env) => {
        console.log('scheduled event...')
    },
}
