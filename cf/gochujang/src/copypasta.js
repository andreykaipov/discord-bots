const sanitize = (s) => s.trim().replace(/([^\n])\n([^\n])/g, '$1 $2')

export const thanks = sanitize(`
blbh

blah bla
`)
