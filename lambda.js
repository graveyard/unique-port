var util = require('util');

exports.external = function(event, context) {

    console.log('event', JSON.stringify(util.inspect(event, {"depth": null}), null, 4));
    console.log('context', JSON.stringify(util.inspect(context, {"depth": null}), null, 4));

    process.on('uncaughtException', function(err) {
        return context.done(err);
    });

    var child = require('child_process').spawn('./uniqueport', [JSON.stringify(event)], { stdio:'inherit' });

    child.on('close', function(code) {
        if (code !== 0 ) {
            return context.done(new Error("Process exited with non-zero status code: " + code));
        } else {
            context.done(null);
        }
    });
}
